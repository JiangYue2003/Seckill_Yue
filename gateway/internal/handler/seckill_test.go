package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	commonpb "seckill-mall/common/common"
	seckillpb "seckill-mall/common/seckill"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
)

type fakeSeckillServiceClient struct {
	seckillpb.SeckillServiceClient

	statusReq    *seckillpb.SeckillStatusRequest
	statusResp   *seckillpb.SeckillStatusResponse
	statusErr    error
	resultReq    *seckillpb.SeckillResultRequest
	resultResp   *seckillpb.SeckillResultResponse
	resultErr    error
}

func (f *fakeSeckillServiceClient) GetSeckillStatus(ctx context.Context, in *seckillpb.SeckillStatusRequest, opts ...grpc.CallOption) (*seckillpb.SeckillStatusResponse, error) {
	f.statusReq = in
	if f.statusErr != nil {
		return nil, f.statusErr
	}
	return f.statusResp, nil
}

func (f *fakeSeckillServiceClient) GetSeckillResult(ctx context.Context, in *seckillpb.SeckillResultRequest, opts ...grpc.CallOption) (*seckillpb.SeckillResultResponse, error) {
	f.resultReq = in
	if f.resultErr != nil {
		return nil, f.resultErr
	}
	return f.resultResp, nil
}

func TestGetSeckillStatusReturnsReservationAndPaymentStates(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &fakeSeckillServiceClient{
		statusResp: &seckillpb.SeckillStatusResponse{
			Status:            "pending",
			OrderId:           "order-1",
			ProductId:         2001,
			Quantity:          1,
			ReservationId:     "res-1",
			ReservationStatus: commonpb.ReservationStatus_RESERVATION_STATUS_ORDER_CREATED,
			OrderStatus:       commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_ORDER_CREATED,
			PaymentStatus:     commonpb.PaymentStatus_PAYMENT_STATUS_REQUESTED,
		},
	}
	handler := NewSeckillHandler(client)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/seckill/status?seckillProductId=101", nil)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("userId", int64(1001))

	handler.GetSeckillStatus(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if client.statusReq == nil {
		t.Fatal("expected GetSeckillStatus rpc to be called")
	}
	if client.statusReq.GetUserId() != 1001 || client.statusReq.GetSeckillProductId() != 101 {
		t.Fatalf("unexpected status req: %+v", client.statusReq)
	}

	var resp struct {
		Code int `json:"code"`
		Data struct {
			Status            string `json:"status"`
			OrderID           string `json:"orderId"`
			ProductID         int64  `json:"productId"`
			Quantity          int64  `json:"quantity"`
			ReservationID     string `json:"reservationId"`
			ReservationStatus string `json:"reservationStatus"`
			OrderStatus       string `json:"orderStatus"`
			PaymentStatus     string `json:"paymentStatus"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Data.Status != "pending" ||
		resp.Data.OrderID != "order-1" ||
		resp.Data.ProductID != 2001 ||
		resp.Data.Quantity != 1 ||
		resp.Data.ReservationID != "res-1" {
		t.Fatalf("unexpected response body: %s", w.Body.String())
	}
	if resp.Data.ReservationStatus != commonpb.ReservationStatus_RESERVATION_STATUS_ORDER_CREATED.String() {
		t.Fatalf("unexpected reservation status: %s", resp.Data.ReservationStatus)
	}
	if resp.Data.OrderStatus != commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_ORDER_CREATED.String() {
		t.Fatalf("unexpected order status: %s", resp.Data.OrderStatus)
	}
	if resp.Data.PaymentStatus != commonpb.PaymentStatus_PAYMENT_STATUS_REQUESTED.String() {
		t.Fatalf("unexpected payment status: %s", resp.Data.PaymentStatus)
	}
}

func TestGetSeckillResultReturnsLifecycleStates(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &fakeSeckillServiceClient{
		resultResp: &seckillpb.SeckillResultResponse{
			Success:           true,
			OrderId:           "order-2",
			ProductId:         3002,
			ProductName:       "keyboard",
			Quantity:          2,
			Amount:            19998,
			Status:            "completed",
			Message:           "success",
			ReservationId:     "res-2",
			ReservationStatus: commonpb.ReservationStatus_RESERVATION_STATUS_CONSUMED,
			OrderStatus:       commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_COMPLETED,
			PaymentStatus:     commonpb.PaymentStatus_PAYMENT_STATUS_SUCCESS,
		},
	}
	handler := NewSeckillHandler(client)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/seckill/result?orderId=order-2", nil)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	handler.GetSeckillResult(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if client.resultReq == nil {
		t.Fatal("expected GetSeckillResult rpc to be called")
	}
	if client.resultReq.GetOrderId() != "order-2" {
		t.Fatalf("unexpected result req: %+v", client.resultReq)
	}

	var resp struct {
		Code int `json:"code"`
		Data struct {
			Success           bool   `json:"success"`
			OrderID           string `json:"orderId"`
			ProductID         int64  `json:"productId"`
			ProductName       string `json:"productName"`
			Quantity          int64  `json:"quantity"`
			Amount            int64  `json:"amount"`
			Status            string `json:"status"`
			Message           string `json:"message"`
			ReservationID     string `json:"reservationId"`
			ReservationStatus string `json:"reservationStatus"`
			OrderStatus       string `json:"orderStatus"`
			PaymentStatus     string `json:"paymentStatus"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !resp.Data.Success ||
		resp.Data.OrderID != "order-2" ||
		resp.Data.ProductID != 3002 ||
		resp.Data.ProductName != "keyboard" ||
		resp.Data.Quantity != 2 ||
		resp.Data.Amount != 19998 ||
		resp.Data.Status != "completed" ||
		resp.Data.Message != "success" ||
		resp.Data.ReservationID != "res-2" {
		t.Fatalf("unexpected response body: %s", w.Body.String())
	}
	if resp.Data.ReservationStatus != commonpb.ReservationStatus_RESERVATION_STATUS_CONSUMED.String() {
		t.Fatalf("unexpected reservation status: %s", resp.Data.ReservationStatus)
	}
	if resp.Data.OrderStatus != commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_COMPLETED.String() {
		t.Fatalf("unexpected order status: %s", resp.Data.OrderStatus)
	}
	if resp.Data.PaymentStatus != commonpb.PaymentStatus_PAYMENT_STATUS_SUCCESS.String() {
		t.Fatalf("unexpected payment status: %s", resp.Data.PaymentStatus)
	}
}
