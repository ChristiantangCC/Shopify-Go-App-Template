package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/bulk"
)

// BulkStarted identifies a started bulk operation.
type BulkStarted struct {
	OperationID string `json:"operationId" doc:"Admin GraphQL id of the bulk operation."`
}

type bulkStartedOutput struct {
	Body BulkStarted
}

// registerBulk adds POST /api/admin/bulk/products, the demo bulk export:
// every product's id and title. Shopify delivers bulk_operations/finish
// when it is done and the registry handler downloads the result.
func (h API) registerBulk(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID:   "startProductsBulk",
		Method:        http.MethodPost,
		Path:          "/bulk/products",
		Summary:       "Start the demo bulk product export",
		DefaultStatus: http.StatusAccepted,
		Errors:        []int{http.StatusUnprocessableEntity, http.StatusBadGateway},
	}, func(ctx context.Context, _ *struct{}) (*bulkStartedOutput, error) {
		session, err := sessionFrom(ctx)
		if err != nil {
			return nil, err
		}
		operationID, err := bulk.Start(ctx, h.Admin, session.Shop, bulk.ProductsQuery)
		var userErr *bulk.UserError
		switch {
		case errors.As(err, &userErr):
			// Usually "a bulk query is already in progress": Shopify's own
			// message, written for the merchant.
			return nil, huma.Error422UnprocessableEntity(userErr.Message)
		case err != nil:
			return nil, adminError(h.Logger, err)
		}
		h.Logger.Info("[Bulk] Export started", "shop", session.Shop, "operation", operationID)
		return &bulkStartedOutput{Body: BulkStarted{OperationID: operationID}}, nil
	})
}
