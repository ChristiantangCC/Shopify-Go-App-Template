// Package graphql holds every Admin API operation, one file per domain.
// Operations are plain documents with typed response structs next to them;
// handlers pass them to admin.Client.Do. Keeping them here, instead of
// inline in handlers, gives one place to audit costs and scopes.
package graphql

// ProductsList pages through products; the example page uses it.
const ProductsList = `query TemplateProductsList($first: Int!, $after: String) {
  products(first: $first, after: $after, sortKey: UPDATED_AT, reverse: true) {
    edges {
      node {
        id
        title
        handle
        status
        totalInventory
      }
      cursor
    }
    pageInfo {
      hasNextPage
      endCursor
    }
  }
}`

// ProductsListResponse is ProductsList's data shape.
type ProductsListResponse struct {
	Products struct {
		Edges []struct {
			Node struct {
				ID             string `json:"id"`
				Title          string `json:"title"`
				Handle         string `json:"handle"`
				Status         string `json:"status"`
				TotalInventory int    `json:"totalInventory"`
			} `json:"node"`
			Cursor string `json:"cursor"`
		} `json:"edges"`
		PageInfo struct {
			HasNextPage bool   `json:"hasNextPage"`
			EndCursor   string `json:"endCursor"`
		} `json:"pageInfo"`
	} `json:"products"`
}
