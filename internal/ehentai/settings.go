package ehentai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

var ErrPortLocked = errors.New("Hentai@Home 设置页暂时锁定端口")

type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type Client struct {
	HTTPClient HTTPDoer
	BaseURL    string
	MemberID   string
	PassHash   string
	ClientID   string
}

func IsPortLocked(err error) bool {
	return errors.Is(err, ErrPortLocked)
}

func (c Client) UpdatePort(ctx context.Context, port int) error {
	settingsURL := c.settingsURL()

	getReq, err := http.NewRequestWithContext(ctx, http.MethodGet, settingsURL, nil)
	if err != nil {
		return fmt.Errorf("创建 Hentai@Home 设置页请求失败: %w", err)
	}
	c.addCookies(getReq)

	getResp, err := c.httpClient().Do(getReq)
	if err != nil {
		return fmt.Errorf("获取 Hentai@Home 设置页失败: %w", err)
	}
	defer getResp.Body.Close()

	if getResp.StatusCode < http.StatusOK || getResp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("获取 Hentai@Home 设置页失败: HTTP %d", getResp.StatusCode)
	}

	form, locked, err := ParseSettingsForm(getResp.Body)
	if err != nil {
		return fmt.Errorf("解析 Hentai@Home 设置页失败: %w", err)
	}
	if locked {
		return ErrPortLocked
	}
	form.Set("f_port", strconv.Itoa(port))

	postReq, err := http.NewRequestWithContext(ctx, http.MethodPost, settingsURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("创建 Hentai@Home 设置页提交请求失败: %w", err)
	}
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	c.addCookies(postReq)

	postResp, err := c.httpClient().Do(postReq)
	if err != nil {
		return fmt.Errorf("提交 Hentai@Home 设置页失败: %w", err)
	}
	defer postResp.Body.Close()

	if postResp.StatusCode < http.StatusOK || postResp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("提交 Hentai@Home 设置页失败: HTTP %d", postResp.StatusCode)
	}

	return nil
}

func ParseSettingsForm(reader io.Reader) (url.Values, bool, error) {
	doc, err := html.Parse(reader)
	if err != nil {
		return nil, false, fmt.Errorf("解析 Hentai@Home 设置页 HTML 失败: %w", err)
	}

	form := url.Values{}
	locked := false
	foundPort := false

	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && strings.EqualFold(node.Data, "input") {
			name, ok := attr(node, "name")
			if !ok || name == "" {
				return
			}
			if name == "f_port" {
				foundPort = true
				if hasAttr(node, "disabled") {
					locked = true
				}
			}

			inputType, _ := attr(node, "type")
			if strings.EqualFold(inputType, "checkbox") {
				if !hasAttr(node, "checked") {
					return
				}
				form.Add(name, checkboxValue(node))
				return
			}

			value, _ := attr(node, "value")
			form.Add(name, value)
		}

		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)

	if !foundPort {
		return nil, locked, errors.New("Hentai@Home 设置页缺少 f_port 字段")
	}

	return form, locked, nil
}

func (c Client) settingsURL() string {
	baseURL := strings.TrimRight(c.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://e-hentai.org"
	}
	return baseURL + "/hentaiathome.php?cid=" + url.QueryEscape(c.ClientID) + "&act=settings"
}

func (c Client) addCookies(req *http.Request) {
	req.AddCookie(&http.Cookie{Name: "ipb_member_id", Value: c.MemberID})
	req.AddCookie(&http.Cookie{Name: "ipb_pass_hash", Value: c.PassHash})
}

func (c Client) httpClient() HTTPDoer {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

func attr(node *html.Node, name string) (string, bool) {
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, name) {
			return attr.Val, true
		}
	}
	return "", false
}

func hasAttr(node *html.Node, name string) bool {
	_, ok := attr(node, name)
	return ok
}

func checkboxValue(node *html.Node) string {
	value, _ := attr(node, "value")
	if value == "" {
		return "on"
	}
	return value
}
