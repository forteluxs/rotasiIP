package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/elazarl/goproxy"
	"github.com/hashicorp/go-retryablehttp"
	"github.com/mubeng/mubeng/common"
	"github.com/mubeng/mubeng/internal/dashboard"
	"github.com/mubeng/mubeng/internal/proxygateway"
	"github.com/mubeng/mubeng/pkg/helper/awsurl"
	"github.com/mubeng/mubeng/pkg/mubeng"
)

// onRequest handles client request
func (p *Proxy) onRequest(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
	if p.Options.Sync {
		mutex.Lock()
		defer mutex.Unlock()
	}

	if (req.URL.Scheme != "http") && (req.URL.Scheme != "https") {
		return req, serverErr(req)
	}

	resChan := make(chan interface{})

	go func(r *http.Request) {
		// Rate limiter / delay — throttle before each request
		limiter.Wait()

		log.Debugf("%s %s %s", r.RemoteAddr, r.Method, r.URL)

		origPath := r.URL.Path
		origHost := r.URL.Host
		origScheme := r.URL.Scheme
		origReqHost := r.Host

		i := 0
		for {
			r.URL.Path = origPath
			r.URL.Host = origHost
			r.URL.Scheme = origScheme
			r.Host = origReqHost

			proxy := p.rotateProxy()

			retryablehttpClient, err := p.getClient(r, proxy)
			if err != nil {
				if p.Options.Stats != nil {
					p.Options.Stats.IncrErr()
				}
				resChan <- err

				return
			}

			retryablehttpRequest, err := retryablehttp.FromRequest(r)
			if err != nil {
				if p.Options.Stats != nil {
					p.Options.Stats.IncrErr()
				}
				resChan <- err

				return
			}

			reqStart := time.Now()
			resp, err := retryablehttpClient.Do(retryablehttpRequest)
			elapsed := time.Since(reqStart)

			// Retry-on-status: auto-rotate and retry regardless of --rotate-on-error
			retryStatus := 0
			if err == nil && len(p.Options.RetryOnStatus) > 0 {
				for _, code := range p.Options.RetryOnStatus {
					if resp.StatusCode == code {
						resp.Body.Close()
						retryStatus = code
						break
					}
				}
			}
			if retryStatus != 0 {
				if p.Options.Stats != nil {
					p.Options.Stats.IncrErr()
				}
				if i < p.Options.MaxErrors || p.Options.MaxErrors <= 0 {
					log.Debugf("%s Status %d → rotating proxy and retrying", r.RemoteAddr, retryStatus)
					i++
					continue
				}
				resChan <- fmt.Errorf("status %d matched --retry-on-status (max errors reached)", retryStatus)
				return
			}

			if err != nil {
				if p.Options.Stats != nil {
					p.Options.Stats.IncrErr()
				}
				if i >= p.Options.MaxErrors && p.Options.MaxErrors >= 0 {
					resChan <- err

					return
				}

				if p.Options.RemoveOnErr {
					p.removeProxy(proxy)

					log.Debugf(
						"%s Removing proxy IP from proxy pool [proxies=%q]",
						r.RemoteAddr, fmt.Sprint(p.Options.ProxyManager.Count()),
					)
				}

				if p.Options.RotateOnErr && (i < p.Options.MaxErrors || p.Options.MaxErrors <= 0) {
					remaining := fmt.Sprint(p.Options.MaxErrors - i)
					if p.Options.MaxErrors <= 0 {
						remaining = "∞"
					}

					log.Debugf(
						"%s Retrying (rotated) %s %s [remaining=%q]",
						r.RemoteAddr, r.Method, r.URL, remaining,
					)

					i++

					continue
				} else {
					resChan <- err

					return
				}
			}

			buf, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				resChan <- err

				return
			}
			resp.Body = io.NopCloser(bytes.NewBuffer(buf))

			if p.Options.Stats != nil {
				p.Options.Stats.IncrReq()
			}

			if entry, err := json.Marshal(map[string]any{
				"t":  time.Now().Format("15:04:05"),
				"m":  r.Method,
				"u":  r.URL.String(),
				"p":  proxy,
				"s":  resp.StatusCode,
				"ms": elapsed.Milliseconds(),
			}); err == nil {
				dashboard.LogBroadcaster.Publish(string(entry))
			}

			resChan <- resp

			return
		}
	}(req)

	var resp *http.Response

	res := <-resChan
	switch res := res.(type) {
	case *http.Response:
		resp = res
		log.Debug(req.RemoteAddr, " ", resp.Status)
	case error:
		err := res
		log.Errorf("%s %s", req.RemoteAddr, err)
		resp = serverErr(req)
	}

	return req, resp
}

// onConnect handles CONNECT method
func (p *Proxy) onConnect(host string, ctx *goproxy.ProxyCtx) (*goproxy.ConnectAction, string) {
	if p.Options.Auth != "" {
		auth := ctx.Req.Header.Get("Proxy-Authorization")
		if auth != "" {
			creds := strings.SplitN(auth, " ", 2)
			if len(creds) != 2 {
				return goproxy.RejectConnect, host
			}

			auth, err := base64.StdEncoding.DecodeString(creds[1])
			if err != nil {
				log.Warnf("%s: Error decoding proxy authorization", ctx.Req.RemoteAddr)
				return goproxy.RejectConnect, host
			}

			if string(auth) != p.Options.Auth {
				log.Errorf("%s: Invalid proxy authorization", ctx.Req.RemoteAddr)
				return goproxy.RejectConnect, host
			}
		} else {
			log.Warnf("%s: Unathorized proxy request to %s", ctx.Req.RemoteAddr, host)
			return goproxy.RejectConnect, host
		}
	}

	return goproxy.MitmConnect, host
}

// onResponse handles backend responses, and removing hop-by-hop headers
func (p *Proxy) onResponse(resp *http.Response, ctx *goproxy.ProxyCtx) *http.Response {
	if resp == nil {
		return resp
	}

	for _, h := range mubeng.HopHeaders {
		resp.Header.Del(h)
	}

	return resp
}

func (p *Proxy) rotateProxy() string {
	rotateMu.Lock()
	defer rotateMu.Unlock()

	if currentProxy == "" || ok >= p.Options.Rotate {
		proxy, err := p.Options.ProxyManager.Rotate(p.Options.Method)
		if err != nil {
			log.Fatalf("Could not rotate proxy IP: %s", err)
		}
		ok = 1
		currentProxy = proxy
		if p.Options.Stats != nil {
			p.Options.Stats.SetCurrentProxy(proxy)
			p.Options.Stats.IncrRotate()
		}
	} else {
		ok++
	}

	return currentProxy
}

func (p *Proxy) removeProxy(target string) {
	rotateMu.Lock()
	defer rotateMu.Unlock()

	err := p.Options.ProxyManager.RemoveProxy(target)
	if err != nil {
		log.Error(err)
		return
	}
	// Force rotation on the next request if we just removed the active proxy
	if target == currentProxy {
		ok = p.Options.Rotate
	}
}

func (p *Proxy) getClient(req *http.Request, proxyAddr string) (*retryablehttp.Client, error) {
	tr, err := mubeng.Transport(proxyAddr)
	if err != nil && !errors.Is(err, mubeng.ErrSwitchTransportAWSProtocolScheme) {
		return nil, err
	}

	proxy := &mubeng.Proxy{
		Address:      proxyAddr,
		MaxRedirects: p.Options.MaxRedirects,
		Timeout:      p.Options.Timeout,
		Transport:    tr,
	}

	client, err := proxy.New(req)
	if err != nil {
		return nil, err
	}

	if awsurl.IsURL(proxyAddr) {
		var pg *proxygateway.ProxyGateway

		awsURL, err := awsurl.Parse(proxyAddr)
		if err != nil {
			return nil, err
		}

		_, err = awsURL.Credentials("")
		if err != nil {
			return nil, err
		}

		accessKeyID := awsURL.AccessKeyID
		secretAccessKey := awsURL.SecretAccessKey
		region := awsURL.Region

		baseURL, _, err := proxygateway.GetBaseURL(req.URL.String())
		if err != nil {
			return nil, err
		}

		gatewayKey := getGatewayKey(baseURL, region)

		p.mu.RLock()
		pg = p.Gateways[gatewayKey]
		p.mu.RUnlock()

		if pg == nil {
			ctx := context.Background()
			gateway, err := proxygateway.New(ctx, accessKeyID, secretAccessKey, region)
			if err != nil {
				return nil, err
			}

			err = gateway.SetBaseURL(baseURL)
			if err != nil {
				return nil, err
			}

			err = gateway.Start(ctx)
			if err != nil {
				return nil, err
			}

			p.mu.Lock()
			if p.Gateways[gatewayKey] == nil {
				p.Gateways[gatewayKey] = gateway
			}
			pg = p.Gateways[gatewayKey]
			p.mu.Unlock()
		}

		// rewrite request URL to API Gateway endpoint URL
		gatewayEndpoint := pg.GetEndpoint()
		req.URL.Path = filepath.Join("/", proxygateway.StageName, req.URL.Path)
		req.URL.Host = gatewayEndpoint.Host
		req.URL.Scheme = gatewayEndpoint.Scheme
		req.Host = gatewayEndpoint.Host
	}

	if p.Options.Verbose {
		client.Transport = dump.RoundTripper(tr)
	}

	retryablehttpClient := mubeng.ToRetryableHTTPClient(client)
	retryablehttpClient.RetryMax = p.Options.MaxRetries
	retryablehttpClient.Logger = ReleveledLogo{
		Logger:  log,
		Request: req,
		Verbose: p.Options.Verbose,
	}

	return retryablehttpClient, nil
}

// nonProxy handles non-proxy requests
func nonProxy(w http.ResponseWriter, req *http.Request) {
	if common.Version != "" {
		w.Header().Add("X-Mubeng-Version", common.Version)
	}

	if req.URL.Path == "/cert" {
		w.Header().Add("Content-Type", "application/octet-stream")
		w.Header().Add("Content-Disposition", fmt.Sprint("attachment; filename=", "goproxy-cacert.der"))
		w.WriteHeader(http.StatusOK)

		if _, err := w.Write(goproxy.GoproxyCa.Certificate[0]); err != nil {
			http.Error(w, "Failed to get proxy certificate authority.", 500)
			log.Errorf("%s %s %s %s", req.RemoteAddr, req.Method, req.URL, err.Error())
		}

		return
	}

	http.Error(w, "This is a mubeng proxy server. Does not respond to non-proxy requests.", 500)
}

func serverErr(req *http.Request) *http.Response {
	return goproxy.NewResponse(req, mime, http.StatusBadGateway, "Proxy server error")
}
