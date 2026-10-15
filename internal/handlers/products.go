package handlers

import (
	"log/slog"
	"net/http"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/admin"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/graphql"
)

// ListProducts is the example data endpoint the home page calls. Shop tells
// it whose products to read, so the same handler serves the embedded API
// (shop from the session token) and the server-to-server API (shop from a
// query parameter).
type ListProducts struct {
	Admin  *admin.Client
	Shop   func(r *http.Request) (string, error)
	Logger *slog.Logger
}

// ServeHTTP handles GET .../products.
func (h ListProducts) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	shop, err := h.Shop(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	variables := map[string]any{"first": 20}
	if after := r.URL.Query().Get("after"); after != "" {
		variables["after"] = after
	}
	var res graphql.ProductsListResponse
	if err := h.Admin.Do(r.Context(), shop, graphql.ProductsList, variables, &res); err != nil {
		failAdmin(w, h.Logger, err)
		return
	}

	type product struct {
		ID             string `json:"id"`
		Title          string `json:"title"`
		Handle         string `json:"handle"`
		Status         string `json:"status"`
		TotalInventory int    `json:"totalInventory"`
	}
	products := make([]product, 0, len(res.Products.Edges))
	for _, edge := range res.Products.Edges {
		products = append(products, product(edge.Node))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"products":    products,
		"hasNextPage": res.Products.PageInfo.HasNextPage,
		"endCursor":   res.Products.PageInfo.EndCursor,
	})
}
