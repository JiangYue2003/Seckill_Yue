package handler

import (
	"context"
	"strconv"
	"time"

	commonpb "seckill-mall/common/common"
	"seckill-mall/common/product"
	"seckill-mall/gateway-hertz/internal/middleware"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// ProductHandler 商品相关接口
type ProductHandler struct {
	productSvc product.ProductServiceClient
}

func NewProductHandler(svc product.ProductServiceClient) *ProductHandler {
	return &ProductHandler{productSvc: svc}
}

// GetProduct 获取商品详情
func (h *ProductHandler) GetProduct(ctx context.Context, c *app.RequestContext) {
	productId, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		middleware.ErrorWithStatus(ctx, c, consts.StatusBadRequest, 400, "无效的商品ID")
		return
	}

	hlog.CtxInfof(ctx, "获取商品详情: productId=%d", productId)

	rpcCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resp, err := h.productSvc.GetProduct(rpcCtx, &product.GetProductRequest{ProductId: productId})
	if err != nil {
		hlog.CtxErrorf(ctx, "获取商品详情失败: %v", err)
		middleware.ErrorWithStatus(ctx, c, consts.StatusInternalServerError, 500, "获取商品详情失败")
		return
	}

	middleware.Success(ctx, c, map[string]interface{}{
		"id":          resp.Id,
		"name":        resp.Name,
		"description": resp.Description,
		"price":       resp.Price,
		"stock":       resp.Stock,
		"soldCount":   resp.SoldCount,
		"coverImage":  resp.CoverImage,
		"status":      resp.Status,
		"createdAt":   resp.CreatedAt,
		"updatedAt":   resp.UpdatedAt,
	})
}

// ListProducts 商品列表
func (h *ProductHandler) ListProducts(ctx context.Context, c *app.RequestContext) {
	page, _ := strconv.ParseInt(string(c.DefaultQuery("page", "1")), 10, 64)
	pageSize, _ := strconv.ParseInt(string(c.DefaultQuery("pageSize", "20")), 10, 64)
	keyword := string(c.DefaultQuery("keyword", ""))
	status, _ := strconv.ParseInt(string(c.DefaultQuery("status", "1")), 10, 64)

	hlog.CtxInfof(ctx, "获取商品列表: page=%d, pageSize=%d, keyword=%s", page, pageSize, keyword)

	rpcCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resp, err := h.productSvc.ListProducts(rpcCtx, &product.ListProductsRequest{
		Keyword:  keyword,
		Status:   int32(status),
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		hlog.CtxErrorf(ctx, "获取商品列表失败: %v", err)
		middleware.ErrorWithStatus(ctx, c, consts.StatusInternalServerError, 500, "获取商品列表失败")
		return
	}

	products := make([]map[string]interface{}, 0, len(resp.Products))
	for _, p := range resp.Products {
		products = append(products, map[string]interface{}{
			"id":          p.Id,
			"name":        p.Name,
			"description": p.Description,
			"price":       p.Price,
			"stock":       p.Stock,
			"soldCount":   p.SoldCount,
			"coverImage":  p.CoverImage,
			"status":      p.Status,
			"createdAt":   p.CreatedAt,
			"updatedAt":   p.UpdatedAt,
		})
	}

	middleware.Success(ctx, c, map[string]interface{}{
		"products": products,
		"total":    resp.Total,
		"page":     resp.Page,
		"pageSize": resp.PageSize,
	})
}

// ListSeckillProducts 秒杀商品列表（gRPC streaming）
func (h *ProductHandler) ListSeckillProducts(ctx context.Context, c *app.RequestContext) {
	hlog.CtxInfof(ctx, "获取秒杀商品列表")

	rpcCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	stream, err := h.productSvc.ListActiveSeckillProducts(rpcCtx, &commonpb.Empty{})
	if err != nil {
		hlog.CtxErrorf(ctx, "获取秒杀商品列表失败: %v", err)
		middleware.ErrorWithStatus(ctx, c, consts.StatusInternalServerError, 500, "获取秒杀商品列表失败")
		return
	}

	products := make([]map[string]interface{}, 0)
	for {
		resp, err := stream.Recv()
		if err != nil {
			break
		}
		products = append(products, map[string]interface{}{
			"id":           resp.Id,
			"productId":    resp.ProductId,
			"seckillPrice": resp.SeckillPrice,
			"seckillStock": resp.SeckillStock,
			"soldCount":    resp.SoldCount,
			"startTime":    resp.StartTime,
			"endTime":      resp.EndTime,
			"perLimit":     resp.PerLimit,
			"status":       resp.Status,
			"productName":  resp.Product.Name,
			"productPrice": resp.Product.Price,
			"productImage": resp.Product.CoverImage,
		})
	}

	middleware.Success(ctx, c, map[string]interface{}{"products": products})
}
