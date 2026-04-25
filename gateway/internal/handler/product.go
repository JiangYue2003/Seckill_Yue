package handler

import (
	"context"
	"net/http"
	"strconv"

	commonpb "seckill-mall/common/common"
	"seckill-mall/common/product"
	"seckill-mall/gateway/internal/middleware"

	"github.com/gin-gonic/gin"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

// ProductHandler 商品相关接口
type ProductHandler struct {
	productSvc product.ProductServiceClient
}

func NewProductHandler(svc product.ProductServiceClient) *ProductHandler {
	return &ProductHandler{productSvc: svc}
}

// GetProduct 获取商品详情
func (h *ProductHandler) GetProduct(c *gin.Context) {
	productIdStr := c.Param("id")
	productId, err := strconv.ParseInt(productIdStr, 10, 64)
	if err != nil {
		middleware.ErrorWithStatus(c, http.StatusBadRequest, 400, "无效的商品ID")
		return
	}

	logx.Infof("获取商品详情: productId=%d", productId)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.productSvc.GetProduct(ctx, &product.GetProductRequest{ProductId: productId})
	if err != nil {
		logx.Errorf("获取商品详情失败: %v", err)
		middleware.ErrorWithStatus(c, http.StatusInternalServerError, 500, "获取商品详情失败")
		return
	}

	middleware.Success(c, gin.H{
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
func (h *ProductHandler) ListProducts(c *gin.Context) {
	page, _ := strconv.ParseInt(c.DefaultQuery("page", "1"), 10, 64)
	pageSize, _ := strconv.ParseInt(c.DefaultQuery("pageSize", "20"), 10, 64)
	keyword := c.DefaultQuery("keyword", "")
	status, _ := strconv.ParseInt(c.DefaultQuery("status", "1"), 10, 64)

	logx.Infof("获取商品列表: page=%d, pageSize=%d, keyword=%s, status=%d", page, pageSize, keyword, status)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.productSvc.ListProducts(ctx, &product.ListProductsRequest{
		Keyword:  keyword,
		Status:   int32(status),
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		logx.Errorf("获取商品列表失败: %v", err)
		middleware.ErrorWithStatus(c, http.StatusInternalServerError, 500, "获取商品列表失败")
		return
	}

	products := make([]gin.H, 0, len(resp.Products))
	for _, p := range resp.Products {
		products = append(products, gin.H{
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

	middleware.Success(c, gin.H{
		"products": products,
		"total":    resp.Total,
		"page":     resp.Page,
		"pageSize": resp.PageSize,
	})
}

// ListSeckillProducts 秒杀商品列表
func (h *ProductHandler) ListSeckillProducts(c *gin.Context) {
	logx.Info("获取秒杀商品列表")

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	stream, err := h.productSvc.ListActiveSeckillProducts(ctx, &commonpb.Empty{})
	if err != nil {
		logx.Errorf("获取秒杀商品列表失败: %v", err)
		middleware.ErrorWithStatus(c, http.StatusInternalServerError, 500, "获取秒杀商品列表失败")
		return
	}

	products := make([]gin.H, 0)
	for {
		resp, err := stream.Recv()
		if err != nil {
			break
		}
		products = append(products, gin.H{
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

	middleware.Success(c, gin.H{
		"products": products,
	})
}
