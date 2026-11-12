// Package channelclient 交易系统调用模拟支付渠道的 HTTP 客户端。
package channelclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"trade/internal/types"
)

type Client struct {
	BaseURL string
	HTTP    *http.Client
}

type refundRequest struct {
	RefundNo string `json:"refund_no"`
	Amount   int64  `json:"amount"`
}

type refundResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		ChannelRefundNo string `json:"channel_refund_no"`
	} `json:"data"`
}

// RequestRefund 请求渠道受理退款，返回渠道退款号；渠道随后异步通知退款结果
func (c *Client) RequestRefund(ctx context.Context, refundNo string, amount int64) (string, error) {
	body, _ := json.Marshal(refundRequest{RefundNo: refundNo, Amount: amount})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/channel/refund", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %v", types.ErrChannel, err)
	}
	defer resp.Body.Close()
	var out refundResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("%w: %v", types.ErrChannel, err)
	}
	if out.Code != 0 {
		return "", fmt.Errorf("%w: %s", types.ErrChannel, out.Msg)
	}
	if out.Data.ChannelRefundNo == "" {
		return "", fmt.Errorf("%w: 响应缺少渠道退款号", types.ErrChannel)
	}
	return out.Data.ChannelRefundNo, nil
}
