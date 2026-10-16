package admin

import (
	"context"

	"github.com/Khan/genqlient/graphql"
)

// ForShop returns a genqlient-compatible client bound to one shop, so the
// generated operations in internal/shopify/graphql run through this
// client's token handling, throttle waits and retry rules:
//
//	res, err := graphql.ProductsList(ctx, adminClient.ForShop(shop), 20, nil)
func (c *Client) ForShop(shop string) graphql.Client {
	return shopClient{client: c, shop: shop}
}

type shopClient struct {
	client *Client
	shop   string
}

// MakeRequest implements genqlient's graphql.Client on top of Do.
func (s shopClient) MakeRequest(ctx context.Context, req *graphql.Request, resp *graphql.Response) error {
	return s.client.Do(ctx, s.shop, req.Query, req.Variables, resp.Data)
}
