//ref: https://github.com/sethvargo/go-fastly/blob/master/client.go
// (c) 2015 Seth Vargo

package deploygate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"

	"github.com/ajg/form"
	cleanhttp "github.com/hashicorp/go-cleanhttp"
	"github.com/mitchellh/mapstructure"
)

// APIKeyEnvVar is the name of the environment variable where the DeployGate API
// key should be read from.
const APIKeyEnvVar = "DEPLOYGATE_API_KEY"

// APIKeyHeader is the name of the header that contains the DeployGate API key.
const APIKeyHeader = "DeployGate-Key"

// DefaultEndpoint is the default endpoint for DeployGate. Since DeployGate does not
// support an on-premise solution, this is likely to always be the default.
const DefaultEndpoint = "https://deploygate.com/api"

// ProjectURL is the url for this library.
var ProjectURL = "github.com/fnaoto/go-deploygate"

// ProjectVersion is the version of this library.
var ProjectVersion = "0.1"

// UserAgent is the user agent for this particular client.
var UserAgent = fmt.Sprintf("DeployGateGo/%s (+%s; %s)",
	ProjectVersion, ProjectURL, runtime.Version())

// Client is the main entrypoint to the DeployGate golang API library.
type Client struct {
	// Address is the address of DeployGate's API endpoint.
	Address string

	// HTTPClient is the HTTP client to use. If one is not provided, a default
	// client will be used.
	HTTPClient *http.Client

	// apiKey is the DeployGate API key to authenticate requests.
	apiKey string

	// url is the parsed URL from Address
	url *url.URL
}

// DefaultClient instantiates a new DeployGate API client. This function requires
// the environment variable `FASTLY_API_KEY` is set and contains a valid API key
// to authenticate with DeployGate.
func DefaultClient() *Client {
	client, err := NewClient(os.Getenv(APIKeyEnvVar))
	if err != nil {
		panic(err)
	}
	return client
}

// NewClient creates a new API client with the given key. Because DeployGate allows
// some requests without an API key, this function will not error if the API
// token is not supplied. Attempts to make a request that requires an API key
// will return a 403 response.
func NewClient(key string) (*Client, error) {
	client := &Client{apiKey: key}
	return client.init()
}

func (c *Client) init() (*Client, error) {
	if len(c.Address) == 0 {
		c.Address = DefaultEndpoint
	}

	u, err := url.Parse(c.Address)
	if err != nil {
		return nil, err
	}
	c.url = u

	if c.HTTPClient == nil {
		c.HTTPClient = cleanhttp.DefaultClient()
	}

	return c, nil
}

// Get issues an HTTP GET request.
func (c *Client) Get(p string, ro *RequestOptions) (*http.Response, error) {
	token := make(map[string]string)
	token["token"] = c.apiKey

	if ro != nil {
		if ro.Params != nil {
			ro.Params["token"] = c.apiKey
		} else {
			ro.Params = token
		}
	} else {
		ro = &RequestOptions{}
		ro.Params = token
	}

	return c.Request("GET", p, ro)
}

// Head issues an HTTP HEAD request.
func (c *Client) Head(p string, ro *RequestOptions) (*http.Response, error) {
	return c.Request("HEAD", p, ro)
}

// Post issues an HTTP POST request.
func (c *Client) Post(p string, ro *RequestOptions) (*http.Response, error) {
	return c.Request("POST", p, ro)
}

// PostForm issues an HTTP POST request with the given interface form-encoded.
func (c *Client) PostForm(p string, i interface{}, ro *RequestOptions) (*http.Response, error) {
	return c.RequestForm("POST", p, i, ro)
}

// Put issues an HTTP PUT request.
func (c *Client) Put(p string, ro *RequestOptions) (*http.Response, error) {
	return c.Request("PUT", p, ro)
}

// PutForm issues an HTTP PUT request with the given interface form-encoded.
func (c *Client) PutForm(p string, i interface{}, ro *RequestOptions) (*http.Response, error) {
	return c.RequestForm("PUT", p, i, ro)
}

// Delete issues an HTTP DELETE request.
func (c *Client) Delete(p string, ro *RequestOptions) (*http.Response, error) {
	return c.Request("DELETE", p, ro)
}

// DeleteForm issues an HTTP DELETE request with the given interface form-encoded.
func (c *Client) DeleteForm(p string, i interface{}, ro *RequestOptions) (*http.Response, error) {
	return c.RequestForm("DELETE", p, i, ro)
}

// Request makes an HTTP request against the HTTPClient using the given verb,
// Path, and request options.
func (c *Client) Request(verb, p string, ro *RequestOptions) (*http.Response, error) {
	req, err := c.RawRequest(verb, p, ro)
	if err != nil {
		return nil, err
	}

	resp, err := checkResp(c.HTTPClient.Do(req))
	if err != nil {
		return resp, err
	}

	return resp, nil
}

// RequestForm makes an HTTP request with the given interface being encoded as
// form data.
func (c *Client) RequestForm(verb, p string, i interface{}, ro *RequestOptions) (*http.Response, error) {
	if ro == nil {
		ro = new(RequestOptions)
	}

	if ro.Headers == nil {
		ro.Headers = make(map[string]string)
	}
	ro.Headers["Content-Type"] = "application/x-www-form-urlencoded"

	buf := new(bytes.Buffer)

	if err := form.NewEncoder(buf).DelimitWith('|').Encode(i); err != nil {
		return nil, err
	}

	tokenParam := fmt.Sprintf("token=%s", c.apiKey)
	body := strings.Join([]string{buf.String(), tokenParam}, "&")

	ro.Body = strings.NewReader(body)
	ro.BodyLength = int64(len(body))

	return c.Request(verb, p, ro)
}

// checkResp wraps an HTTP request from the default client and verifies that the
// request was successful. A non-200 request returns an error formatted to
// included any validation problems or otherwise.
func checkResp(resp *http.Response, err error) (*http.Response, error) {
	// If the err is already there, there was an error higher up the chain, so
	// just return that.
	if err != nil {
		return resp, err
	}

	switch resp.StatusCode {
	case 200, 201, 202, 204, 205, 206:
		return resp, nil
	default:
		return resp, NewHTTPError(resp)
	}
}

// decodeJSON is used to decode an HTTP response body into an interface as JSON.
func decodeJSON(out interface{}, body io.ReadCloser) error {
	defer body.Close()

	var parsed interface{}
	dec := json.NewDecoder(body)
	if err := dec.Decode(&parsed); err != nil {
		return err
	}

	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		DecodeHook: mapstructure.ComposeDecodeHookFunc(
			mapToHTTPHeaderHookFunc(),
			stringToTimeHookFunc(),
		),
		WeaklyTypedInput: true,
		Result:           out,
	})
	if err != nil {
		return err
	}
	return decoder.Decode(parsed)
}
