// Command sailbox is a small REST client for Sailbox, authenticated with an API key.
// It is meant for scripts and AI agents: every command prints the API's JSON to
// stdout, errors go to stderr, and the exit code says whether the call succeeded.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"time"
)

const usage = `sailbox - Sailbox REST API client (API key auth)

Environment:
  SAILBOX_URL         Dashboard base URL, e.g. https://sailbox.example.com
  SAILBOX_API_KEY     API key (sbk_...), created in the dashboard under API Keys
  SAILBOX_ALLOW_HTTP  Set to 1 to allow plain http to a public host (not recommended)

Commands:
  status                          Check that the URL and key work
  projects                        List projects
  apps [--project ID] [--search S] [--status S]
                                  List apps (all, or of one project)
  app <app-id>                    Show one app
  deploy <app-id> [--force-build] [--wait] [--timeout 15m]
                                  Trigger a deployment; --wait polls until it finishes
  deployments <app-id>            List an app's deployments
  deployment <deployment-id>      Show one deployment
  restart <app-id>                Restart an app
  api <METHOD> <PATH> [-d JSON|@file|-]
                                  Call any endpoint; PATH is relative to /api/v1
                                  e.g. sailbox api POST /apps/<id>/scale -d '{"replicas":2}'

Output is the API's JSON on stdout. Exit codes: 0 success, 1 API or network
error (details on stderr), 2 usage error.
`

var errUsage = errors.New("usage")

var pollEvery = 3 * time.Second

func main() {
	err := run(os.Args[1:], os.Stdin, os.Stdout)
	switch {
	case err == nil:
	case errors.Is(err, errUsage):
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	default:
		fmt.Fprintln(os.Stderr, "sailbox:", err)
		os.Exit(1)
	}
}

func run(args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprint(stdout, usage)
		return nil
	}
	c, err := newClient(os.Getenv("SAILBOX_URL"), os.Getenv("SAILBOX_API_KEY"), os.Getenv("SAILBOX_ALLOW_HTTP") == "1")
	if err != nil {
		return err
	}

	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	project := fs.String("project", "", "")
	search := fs.String("search", "", "")
	status := fs.String("status", "", "")
	forceBuild := fs.Bool("force-build", false, "")
	wait := fs.Bool("wait", false, "")
	timeout := fs.Duration("timeout", 15*time.Minute, "")
	data := fs.String("d", "", "")
	pos, err := parseInterleaved(fs, args[1:])
	if err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	need := func(n int) error {
		if len(pos) != n {
			return errUsage
		}
		return nil
	}

	var out []byte
	switch args[0] {
	case "status":
		if err = need(0); err == nil {
			out, err = c.do("GET", "/status", nil)
		}
	case "projects":
		if err = need(0); err == nil {
			out, err = c.do("GET", "/projects", nil)
		}
	case "apps":
		if err = need(0); err == nil {
			q := url.Values{}
			for k, v := range map[string]string{"search": *search, "status": *status} {
				if v != "" {
					q.Set(k, v)
				}
			}
			p := "/apps"
			if *project != "" {
				p = "/projects/" + url.PathEscape(*project) + "/apps"
			}
			if len(q) > 0 {
				p += "?" + q.Encode()
			}
			out, err = c.do("GET", p, nil)
		}
	case "app":
		if err = need(1); err == nil {
			out, err = c.do("GET", "/apps/"+url.PathEscape(pos[0]), nil)
		}
	case "deployments":
		if err = need(1); err == nil {
			out, err = c.do("GET", "/apps/"+url.PathEscape(pos[0])+"/deployments", nil)
		}
	case "deployment":
		if err = need(1); err == nil {
			out, err = c.do("GET", "/deployments/"+url.PathEscape(pos[0]), nil)
		}
	case "restart":
		if err = need(1); err == nil {
			out, err = c.do("POST", "/apps/"+url.PathEscape(pos[0])+"/restart", nil)
		}
	case "deploy":
		if err = need(1); err == nil {
			body := fmt.Sprintf(`{"force_build":%t}`, *forceBuild)
			out, err = c.do("POST", "/apps/"+url.PathEscape(pos[0])+"/deploy", strings.NewReader(body))
			if err == nil && *wait {
				out, err = c.waitDeploy(out, *timeout)
			}
		}
	case "api":
		if err = need(2); err == nil {
			var body io.Reader
			if body, err = readData(*data, stdin); err == nil {
				out, err = c.do(strings.ToUpper(pos[0]), pos[1], body)
			}
		}
	default:
		return fmt.Errorf("%w: unknown command %q", errUsage, args[0])
	}
	if len(out) > 0 {
		writeJSON(stdout, out)
	}
	return err
}

// parseInterleaved lets flags appear before or after positional arguments.
func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos, args = append(pos, args[0]), args[1:]
	}
}

func readData(d string, stdin io.Reader) (io.Reader, error) {
	switch {
	case d == "":
		return nil, nil
	case d == "-":
		return stdin, nil
	case strings.HasPrefix(d, "@"):
		return os.Open(d[1:])
	default:
		return strings.NewReader(d), nil
	}
}

type client struct {
	base string
	key  string
	http *http.Client
}

func newClient(rawURL, key string, allowHTTP bool) (*client, error) {
	if rawURL == "" || key == "" {
		return nil, errors.New("SAILBOX_URL and SAILBOX_API_KEY must be set")
	}
	u, err := url.Parse(strings.TrimRight(rawURL, "/"))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("SAILBOX_URL %q must look like https://host", rawURL)
	}
	// The key is a bearer credential: refuse to send it in clear text over the internet.
	if u.Scheme == "http" && !allowHTTP && !privateHost(u.Hostname()) {
		return nil, fmt.Errorf("refusing to send the API key over plain http to %s; use https or set SAILBOX_ALLOW_HTTP=1", u.Hostname())
	}
	return &client{base: u.String(), key: key, http: &http.Client{Timeout: 60 * time.Second}}, nil
}

// privateHost reports hosts where plain http does not cross the public internet:
// loopback, LAN, and Tailscale (100.64.0.0/10, *.ts.net, which WireGuard encrypts).
func privateHost(h string) bool {
	if h == "localhost" || strings.HasSuffix(h, ".localhost") || strings.HasSuffix(h, ".ts.net") {
		return true
	}
	ip, err := netip.ParseAddr(h)
	if err != nil {
		if ips, lerr := net.LookupHost(h); lerr == nil && len(ips) > 0 {
			ip, err = netip.ParseAddr(ips[0])
		}
	}
	if err != nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || netip.MustParsePrefix("100.64.0.0/10").Contains(ip)
}

func (c *client) do(method, path string, body io.Reader) ([]byte, error) {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if !strings.HasPrefix(path, "/api/") {
		path = "/api/v1" + path
	}
	req, err := http.NewRequest(method, c.base+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return out, fmt.Errorf("%s %s: HTTP %d", method, path, resp.StatusCode)
	}
	return out, nil
}

func (c *client) waitDeploy(created []byte, timeout time.Duration) ([]byte, error) {
	var d struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(created, &d); err != nil || d.ID == "" {
		return created, errors.New("deploy response has no deployment id to wait on")
	}
	deadline := time.Now().Add(timeout)
	out := created
	for {
		switch d.Status {
		case "success":
			return out, nil
		case "failed", "cancelled":
			return out, fmt.Errorf("deployment %s %s", d.ID, d.Status)
		}
		if time.Now().After(deadline) {
			return out, fmt.Errorf("deployment %s still %s after %s", d.ID, d.Status, timeout)
		}
		time.Sleep(pollEvery)
		var err error
		if out, err = c.do("GET", "/deployments/"+d.ID, nil); err != nil {
			return out, err
		}
		if err := json.Unmarshal(out, &d); err != nil {
			return out, err
		}
	}
}

func writeJSON(w io.Writer, b []byte) {
	var buf bytes.Buffer
	if json.Indent(&buf, b, "", "  ") == nil {
		b = buf.Bytes()
	}
	w.Write(b)
	if !bytes.HasSuffix(b, []byte("\n")) {
		fmt.Fprintln(w)
	}
}
