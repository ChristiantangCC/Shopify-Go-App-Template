package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/graphql"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/shopdomain"
)

// Product is one row of the example products listing.
type Product struct {
	ID             string `json:"id" doc:"Admin GraphQL id."`
	Title          string `json:"title"`
	Handle         string `json:"handle"`
	Status         string `json:"status" doc:"ACTIVE, ARCHIVED or DRAFT."`
	TotalInventory int    `json:"totalInventory"`
}

// ProductsPage is one page of products, with the cursor for the next.
type ProductsPage struct {
	Products    []Product `json:"products"`
	HasNextPage bool      `json:"hasNextPage"`
	EndCursor   string    `json:"endCursor" doc:"Pass as ?after= for the next page; empty on the last."`
}

type productsOutput struct {
	Body ProductsPage
}

type productsInput struct {
	After string `query:"after" doc:"Cursor from the previous page's endCursor."`
}

type serviceProductsInput struct {
	Shop  string `query:"shop" required:"true" doc:"The shop's myshopify domain." example:"example-shop.myshopify.com"`
	After string `query:"after" doc:"Cursor from the previous page's endCursor."`
}

// registerProducts adds GET /api/admin/products, the example data
// endpoint: the shop comes from the session token.
func (h API) registerProducts(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "listProducts",
		Method:      http.MethodGet,
		Path:        "/products",
		Summary:     "List the shop's products, 20 per page",
		Errors:      []int{http.StatusBadGateway},
	}, func(ctx context.Context, input *productsInput) (*productsOutput, error) {
		session, err := sessionFrom(ctx)
		if err != nil {
			return nil, err
		}
		return h.listProducts(ctx, session.Shop, input.After)
	})
}

// registerServiceProducts adds GET /api/service/products: the same listing
// for server-to-server callers, who name the shop.
func (h API) registerServiceProducts(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "serviceListProducts",
		Method:      http.MethodGet,
		Path:        "/products",
		Summary:     "List a shop's products, 20 per page",
		Errors:      []int{http.StatusBadRequest, http.StatusBadGateway},
	}, func(ctx context.Context, input *serviceProductsInput) (*productsOutput, error) {
		if !shopdomain.Valid(input.Shop) {
			return nil, huma.Error400BadRequest("shop must be a <shop>.myshopify.com domain")
		}
		return h.listProducts(ctx, input.Shop, input.After)
	})
}

func (h API) listProducts(ctx context.Context, shop, after string) (*productsOutput, error) {
	var cursor *string
	if after != "" {
		cursor = &after
	}
	res, err := graphql.ProductsList(ctx, h.Admin.ForShop(shop), 20, cursor)
	if err != nil {
		return nil, adminError(h.Logger, err)
	}
	page := ProductsPage{Products: make([]Product, 0, len(res.Products.Edges)), HasNextPage: res.Products.PageInfo.HasNextPage}
	for _, edge := range res.Products.Edges {
		page.Products = append(page.Products, Product{
			ID:             edge.Node.Id,
			Title:          edge.Node.Title,
			Handle:         edge.Node.Handle,
			Status:         string(edge.Node.Status),
			TotalInventory: edge.Node.TotalInventory,
		})
	}
	if res.Products.PageInfo.EndCursor != nil {
		page.EndCursor = *res.Products.PageInfo.EndCursor
	}
	return &productsOutput{Body: page}, nil
}
