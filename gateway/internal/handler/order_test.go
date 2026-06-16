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

	getOrderReq       *orderpb.GetOrderRequest
	getOrderResp      *orderpb.OrderInfo
	getOrderErr       error
	listOrdersReq     *orderpb.ListUserOrdersRequest
	listOrdersResp    *orderpb.ListUserOrdersResponse
	listOrdersErr     error
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

func (f *fakeOrderServiceClient) GetOrder(ctx context.Context, in *orderpb.GetOrderRequest, opts ...grpc.CallOption) (*orderpb.OrderInfo, error) {
	f.getOrderReq = in
	if f.getOrderErr != nil {
		return nil, f.getOrderErr
	}
	return f.getOrderResp, nil
}

func (f *fakeOrderServiceClient) ListUserOrders(ctx context.Context, in *orderpb.ListUserOrdersRequest, opts ...grpc.CallOption) (*orderpb.ListUserOrdersResponse, error) {
	f.listOrdersReq = in
	if f.listOrdersErr != nil {
		return nil, f.listOrdersErr
	}
	return f.listOrdersResp, nil
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

func TestPayOrderIgnoresLegacyPaymentIDField(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &fakeOrderServiceClient{
		payOrderResp: &commonpb.BoolResponse{Success: true, Message: "支付成功"},
	}
	handler := NewOrderHandler(client)

	body := `{"orderId":"order-legacy-1","paymentId":"legacy-pay-1","channel":"mock_alipay","requestId":"req-legacy-1"}`
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
	if client.payOrderReq.GetOrderId() != "order-legacy-1" ||
		client.payOrderReq.GetChannel() != "mock_alipay" ||
		client.payOrderReq.GetRequestId() != "req-legacy-1" {
		t.Fatalf("unexpected pay order req: %+v", client.payOrderReq)
	}
	if client.payOrderReq.GetPaymentId() != "" {
		t.Fatalf("expected legacy paymentId to be ignored, got %q", client.payOrderReq.GetPaymentId())
	}

	var resp struct {
		Code int `json:"code"`
		Data struct {
			Success   bool   `json:"success"`
			Message   string `json:"message"`
			OrderID   string `json:"orderId"`
			Channel   string `json:"channel"`
			RequestID string `json:"requestId"`
			PaymentID string `json:"paymentId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !resp.Data.Success || resp.Data.OrderID != "order-legacy-1" || resp.Data.Channel != "mock_alipay" || resp.Data.RequestID != "req-legacy-1" {
		t.Fatalf("unexpected response body: %s", w.Body.String())
	}
	if resp.Data.PaymentID != "" {
		t.Fatalf("expected response not to expose legacy paymentId semantics, got body: %s", w.Body.String())
	}
}

func TestGetOrderKeepsLegacyResponseShape(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &fakeOrderServiceClient{
		getOrderResp: &orderpb.OrderInfo{
			OrderId:           "order-10",
			UserId:            1001,
			ProductId:         3001,
			ProductName:       "keyboard",
			Quantity:          2,
			Amount:            19998,
			SeckillPrice:      9999,
			OrderType:         int32(orderpb.OrderType_ORDER_TYPE_SECKILL),
			Status:            int32(orderpb.OrderStatus_ORDER_STATUS_COMPLETED),
			PaymentId:         "pay-10",
			ReservationId:     "res-10",
			ReservationStatus: commonpb.ReservationStatus_RESERVATION_STATUS_CONSUMED,
			PaymentStatus:     commonpb.PaymentStatus_PAYMENT_STATUS_SUCCESS,
			PaidAt:            1710000001,
			ExpiredAt:         1710000301,
			CreatedAt:         1710000000,
			UpdatedAt:         1710000002,
		},
	}
	handler := NewOrderHandler(client)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/order/order-10", nil)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Params = gin.Params{{Key: "orderId", Value: "order-10"}}
	c.Set("userId", int64(1001))

	handler.GetOrder(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if client.getOrderReq == nil || client.getOrderReq.GetOrderId() != "order-10" {
		t.Fatalf("unexpected get order req: %+v", client.getOrderReq)
	}

	var resp struct {
		Code int `json:"code"`
		Data struct {
			OrderID           string `json:"orderId"`
			UserID            int64  `json:"userId"`
			ProductID         int64  `json:"productId"`
			ProductName       string `json:"productName"`
			Quantity          int64  `json:"quantity"`
			Amount            int64  `json:"amount"`
			SeckillPrice      int64  `json:"seckillPrice"`
			OrderType         int32  `json:"orderType"`
			Status            int32  `json:"status"`
			PaymentID         string `json:"paymentId"`
			ReservationID     string `json:"reservationId"`
			ReservationStatus string `json:"reservationStatus"`
			PaymentStatus     string `json:"paymentStatus"`
			PaidAt            int64  `json:"paidAt"`
			ExpiredAt         int64  `json:"expiredAt"`
			CreatedAt         int64  `json:"createdAt"`
			UpdatedAt         int64  `json:"updatedAt"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Data.OrderID != "order-10" ||
		resp.Data.UserID != 1001 ||
		resp.Data.ProductID != 3001 ||
		resp.Data.ProductName != "keyboard" ||
		resp.Data.Quantity != 2 ||
		resp.Data.Amount != 19998 ||
		resp.Data.SeckillPrice != 9999 ||
		resp.Data.OrderType != int32(orderpb.OrderType_ORDER_TYPE_SECKILL) ||
		resp.Data.Status != int32(orderpb.OrderStatus_ORDER_STATUS_COMPLETED) ||
		resp.Data.PaymentID != "pay-10" ||
		resp.Data.ReservationID != "res-10" ||
		resp.Data.PaidAt != 1710000001 ||
		resp.Data.ExpiredAt != 1710000301 ||
		resp.Data.CreatedAt != 1710000000 ||
		resp.Data.UpdatedAt != 1710000002 {
		t.Fatalf("unexpected response body: %s", w.Body.String())
	}
	if resp.Data.ReservationStatus != commonpb.ReservationStatus_RESERVATION_STATUS_CONSUMED.String() {
		t.Fatalf("unexpected reservation status: %s", resp.Data.ReservationStatus)
	}
	if resp.Data.PaymentStatus != commonpb.PaymentStatus_PAYMENT_STATUS_SUCCESS.String() {
		t.Fatalf("unexpected payment status: %s", resp.Data.PaymentStatus)
	}
}

func TestListOrdersKeepsLegacyListShape(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &fakeOrderServiceClient{
		listOrdersResp: &orderpb.ListUserOrdersResponse{
			Orders: []*orderpb.OrderInfo{
				{
					OrderId:           "order-20",
					ProductId:         3002,
					ProductName:       "mouse",
					Quantity:          1,
					Amount:            5999,
					OrderType:         int32(orderpb.OrderType_ORDER_TYPE_NORMAL),
					Status:            int32(orderpb.OrderStatus_ORDER_STATUS_PENDING),
					ReservationId:     "",
					ReservationStatus: commonpb.ReservationStatus_RESERVATION_STATUS_RESERVED,
					PaymentStatus:     commonpb.PaymentStatus_PAYMENT_STATUS_INIT,
					CreatedAt:         1710000100,
				},
			},
			Total: 1,
		},
	}
	handler := NewOrderHandler(client)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/orders?page=2&pageSize=5&status=1", nil)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("userId", int64(1002))

	handler.ListOrders(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if client.listOrdersReq == nil {
		t.Fatal("expected ListUserOrders rpc to be called")
	}
	if client.listOrdersReq.GetUserId() != 1002 || client.listOrdersReq.GetPage() != 2 || client.listOrdersReq.GetPageSize() != 5 || client.listOrdersReq.GetStatus() != 1 {
		t.Fatalf("unexpected list orders req: %+v", client.listOrdersReq)
	}

	var resp struct {
		Code int `json:"code"`
		Data struct {
			Orders []struct {
				OrderID           string `json:"orderId"`
				ProductID         int64  `json:"productId"`
				ProductName       string `json:"productName"`
				Quantity          int64  `json:"quantity"`
				Amount            int64  `json:"amount"`
				OrderType         int32  `json:"orderType"`
				Status            int32  `json:"status"`
				ReservationID     string `json:"reservationId"`
				ReservationStatus string `json:"reservationStatus"`
				PaymentStatus     string `json:"paymentStatus"`
				CreatedAt         int64  `json:"createdAt"`
			} `json:"orders"`
			Total    int64 `json:"total"`
			Page     int64 `json:"page"`
			PageSize int64 `json:"pageSize"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(resp.Data.Orders) != 1 {
		t.Fatalf("expected one order, got body: %s", w.Body.String())
	}
	orderItem := resp.Data.Orders[0]
	if orderItem.OrderID != "order-20" ||
		orderItem.ProductID != 3002 ||
		orderItem.ProductName != "mouse" ||
		orderItem.Quantity != 1 ||
		orderItem.Amount != 5999 ||
		orderItem.OrderType != int32(orderpb.OrderType_ORDER_TYPE_NORMAL) ||
		orderItem.Status != int32(orderpb.OrderStatus_ORDER_STATUS_PENDING) ||
		orderItem.CreatedAt != 1710000100 {
		t.Fatalf("unexpected order item: %+v", orderItem)
	}
	if orderItem.ReservationStatus != commonpb.ReservationStatus_RESERVATION_STATUS_RESERVED.String() {
		t.Fatalf("unexpected reservation status: %s", orderItem.ReservationStatus)
	}
	if orderItem.PaymentStatus != commonpb.PaymentStatus_PAYMENT_STATUS_INIT.String() {
		t.Fatalf("unexpected payment status: %s", orderItem.PaymentStatus)
	}
	if resp.Data.Total != 1 || resp.Data.Page != 2 || resp.Data.PageSize != 5 {
		t.Fatalf("unexpected list response body: %s", w.Body.String())
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

func TestHandleMockPaymentCallbackForwardsPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &fakeOrderServiceClient{
		callbackResp: &commonpb.BoolResponse{Success: true, Message: "回调处理成功"},
	}
	handler := NewOrderHandler(client)

	body := `{"paymentId":"pay-3","orderId":"order-3","callbackId":"cb-3","channel":"mock_alipay","thirdPartyTradeNo":"trade-3","rawPayload":"{\"ok\":true}"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/payment/callback/mock", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	handler.HandleMockPaymentCallback(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if client.callbackReq == nil {
		t.Fatal("expected HandlePaymentCallback rpc to be called")
	}
	if client.callbackReq.GetPaymentId() != "pay-3" ||
		client.callbackReq.GetOrderId() != "order-3" ||
		client.callbackReq.GetCallbackId() != "cb-3" ||
		client.callbackReq.GetChannel() != "mock_alipay" ||
		client.callbackReq.GetThirdPartyTradeNo() != "trade-3" ||
		client.callbackReq.GetRawPayload() != "{\"ok\":true}" {
		t.Fatalf("unexpected callback req: %+v", client.callbackReq)
	}

	var resp struct {
		Code int `json:"code"`
		Data struct {
			Success bool   `json:"success"`
			Message string `json:"message"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !resp.Data.Success || resp.Data.Message != "回调处理成功" {
		t.Fatalf("unexpected response body: %s", w.Body.String())
	}
}
