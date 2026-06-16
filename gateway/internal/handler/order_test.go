package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	commonpb "seckill-mall/common/common"
	orderpb "seckill-mall/common/order"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
)

type fakeOrderServiceClient struct {
	orderpb.OrderServiceClient

	payOrderReq       *orderpb.PayOrderRequest
	payOrderResp      *commonpb.BoolResponse
	payOrderErr       error
	createPaymentReq  *orderpb.CreatePaymentRequest
	createPaymentResp *orderpb.CreatePaymentResponse
	createPaymentErr  error
	getPaymentReq     *orderpb.GetPaymentRequest
	getPaymentResp    *orderpb.PaymentInfo
	getPaymentErr     error
	callbackReq       *orderpb.HandlePaymentCallbackRequest
	callbackResp      *commonpb.BoolResponse
	callbackErr       error
}

func (f *fakeOrderServiceClient) PayOrder(ctx context.Context, in *orderpb.PayOrderRequest, opts ...grpc.CallOption) (*commonpb.BoolResponse, error) {
	f.payOrderReq = in
	if f.payOrderErr != nil {
		return nil, f.payOrderErr
	}
	return f.payOrderResp, nil
}

func (f *fakeOrderServiceClient) CreatePayment(ctx context.Context, in *orderpb.CreatePaymentRequest, opts ...grpc.CallOption) (*orderpb.CreatePaymentResponse, error) {
	f.createPaymentReq = in
	if f.createPaymentErr != nil {
		return nil, f.createPaymentErr
	}
	return f.createPaymentResp, nil
}

func (f *fakeOrderServiceClient) GetPayment(ctx context.Context, in *orderpb.GetPaymentRequest, opts ...grpc.CallOption) (*orderpb.PaymentInfo, error) {
	f.getPaymentReq = in
	if f.getPaymentErr != nil {
		return nil, f.getPaymentErr
	}
	return f.getPaymentResp, nil
}

func (f *fakeOrderServiceClient) HandlePaymentCallback(ctx context.Context, in *orderpb.HandlePaymentCallbackRequest, opts ...grpc.CallOption) (*commonpb.BoolResponse, error) {
	f.callbackReq = in
	if f.callbackErr != nil {
		return nil, f.callbackErr
	}
	return f.callbackResp, nil
}

func TestPayOrderDoesNotRequirePaymentID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &fakeOrderServiceClient{
		payOrderResp: &commonpb.BoolResponse{Success: true, Message: "支付成功"},
	}
	handler := NewOrderHandler(client)

	body := `{"orderId":"order-1","channel":"mock_alipay","requestId":"req-1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/order/pay", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("userId", int64(1001))

	handler.PayOrder(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if client.payOrderReq == nil {
		t.Fatal("expected PayOrder rpc to be called")
	}
	if client.payOrderReq.GetPaymentId() != "" {
		t.Fatalf("expected paymentId omitted, got %q", client.payOrderReq.GetPaymentId())
	}
	if client.payOrderReq.GetOrderId() != "order-1" || client.payOrderReq.GetChannel() != "mock_alipay" || client.payOrderReq.GetRequestId() != "req-1" {
		t.Fatalf("unexpected pay order req: %+v", client.payOrderReq)
	}
}

func TestCreatePaymentReturnsPaymentPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &fakeOrderServiceClient{
		createPaymentResp: &orderpb.CreatePaymentResponse{
			Success: true,
			Message: "支付单创建成功",
			Payment: &orderpb.PaymentInfo{
				PaymentId: "pay-1",
				OrderId:   "order-1",
				UserId:    1001,
				Amount:    9900,
				Channel:   "mock_alipay",
				Status:    commonpb.PaymentStatus_PAYMENT_STATUS_SUCCESS,
				RequestId: "req-1",
			},
		},
	}
	handler := NewOrderHandler(client)

	body := `{"orderId":"order-1","channel":"mock_alipay","requestId":"req-1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/payment", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("userId", int64(1001))

	handler.CreatePayment(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if client.createPaymentReq == nil {
		t.Fatal("expected CreatePayment rpc to be called")
	}
	if client.createPaymentReq.GetOrderId() != "order-1" || client.createPaymentReq.GetChannel() != "mock_alipay" || client.createPaymentReq.GetRequestId() != "req-1" {
		t.Fatalf("unexpected create payment req: %+v", client.createPaymentReq)
	}

	var resp struct {
		Code int `json:"code"`
		Data struct {
			Success bool   `json:"success"`
			Message string `json:"message"`
			Payment struct {
				PaymentID string `json:"paymentId"`
				OrderID   string `json:"orderId"`
				Status    string `json:"status"`
				RequestID string `json:"requestId"`
			} `json:"payment"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !resp.Data.Success || resp.Data.Payment.PaymentID != "pay-1" || resp.Data.Payment.OrderID != "order-1" {
		t.Fatalf("unexpected response body: %s", w.Body.String())
	}
	if resp.Data.Payment.Status != commonpb.PaymentStatus_PAYMENT_STATUS_SUCCESS.String() {
		t.Fatalf("unexpected payment status: %s", resp.Data.Payment.Status)
	}
}

func TestGetPaymentReturnsPaymentPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &fakeOrderServiceClient{
		getPaymentResp: &orderpb.PaymentInfo{
			PaymentId:         "pay-2",
			OrderId:           "order-2",
			UserId:            1002,
			Amount:            19900,
			Channel:           "mock_alipay",
			Status:            commonpb.PaymentStatus_PAYMENT_STATUS_SUCCESS,
			ThirdPartyTradeNo: "trade-2",
			RequestId:         "req-2",
		},
	}
	handler := NewOrderHandler(client)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/payment?paymentId=pay-2&orderId=order-2", nil)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("userId", int64(1002))

	handler.GetPayment(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if client.getPaymentReq == nil {
		t.Fatal("expected GetPayment rpc to be called")
	}
	if client.getPaymentReq.GetPaymentId() != "pay-2" || client.getPaymentReq.GetOrderId() != "order-2" {
		t.Fatalf("unexpected get payment req: %+v", client.getPaymentReq)
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			PaymentID string `json:"paymentId"`
			OrderID   string `json:"orderId"`
			Status    string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Data.PaymentID != "pay-2" || resp.Data.OrderID != "order-2" {
		t.Fatalf("unexpected response body: %s", w.Body.String())
	}
	if resp.Data.Status != commonpb.PaymentStatus_PAYMENT_STATUS_SUCCESS.String() {
		t.Fatalf("unexpected payment status: %s", resp.Data.Status)
	}
}
