package graphql

// WebhookSubscriptionsList lists the app's current subscriptions, used to
// sync dynamic subscriptions idempotently.
const WebhookSubscriptionsList = `query TemplateWebhookSubscriptions($first: Int!, $after: String) {
  webhookSubscriptions(first: $first, after: $after) {
    edges {
      node {
        id
        topic
        endpoint {
          __typename
          ... on WebhookHttpEndpoint {
            callbackUrl
          }
        }
      }
      cursor
    }
    pageInfo {
      hasNextPage
      endCursor
    }
  }
}`

// WebhookSubscriptionsListResponse is WebhookSubscriptionsList's data shape.
type WebhookSubscriptionsListResponse struct {
	WebhookSubscriptions struct {
		Edges []struct {
			Node struct {
				ID       string `json:"id"`
				Topic    string `json:"topic"`
				Endpoint struct {
					Typename    string `json:"__typename"`
					CallbackURL string `json:"callbackUrl"`
				} `json:"endpoint"`
			} `json:"node"`
			Cursor string `json:"cursor"`
		} `json:"edges"`
		PageInfo struct {
			HasNextPage bool   `json:"hasNextPage"`
			EndCursor   string `json:"endCursor"`
		} `json:"pageInfo"`
	} `json:"webhookSubscriptions"`
}

// WebhookSubscriptionCreate subscribes a shop to one topic at a callback URL.
const WebhookSubscriptionCreate = `mutation TemplateWebhookSubscriptionCreate($topic: WebhookSubscriptionTopic!, $callbackUrl: URL!) {
  webhookSubscriptionCreate(topic: $topic, webhookSubscription: {callbackUrl: $callbackUrl, format: JSON}) {
    webhookSubscription {
      id
    }
    userErrors {
      field
      message
    }
  }
}`

// WebhookSubscriptionUpdate moves an existing subscription to a new callback
// URL (a dev tunnel changed, for example).
const WebhookSubscriptionUpdate = `mutation TemplateWebhookSubscriptionUpdate($id: ID!, $callbackUrl: URL!) {
  webhookSubscriptionUpdate(id: $id, webhookSubscription: {callbackUrl: $callbackUrl, format: JSON}) {
    webhookSubscription {
      id
    }
    userErrors {
      field
      message
    }
  }
}`

// WebhookSubscriptionDelete removes a subscription this app no longer wants.
const WebhookSubscriptionDelete = `mutation TemplateWebhookSubscriptionDelete($id: ID!) {
  webhookSubscriptionDelete(id: $id) {
    deletedWebhookSubscriptionId
    userErrors {
      field
      message
    }
  }
}`

// MutationUserError is the shared userErrors entry shape.
type MutationUserError struct {
	Field   []string `json:"field"`
	Message string   `json:"message"`
}

// WebhookSubscriptionCreateResponse is WebhookSubscriptionCreate's data shape.
type WebhookSubscriptionCreateResponse struct {
	WebhookSubscriptionCreate struct {
		WebhookSubscription *struct {
			ID string `json:"id"`
		} `json:"webhookSubscription"`
		UserErrors []MutationUserError `json:"userErrors"`
	} `json:"webhookSubscriptionCreate"`
}

// WebhookSubscriptionUpdateResponse is WebhookSubscriptionUpdate's data shape.
type WebhookSubscriptionUpdateResponse struct {
	WebhookSubscriptionUpdate struct {
		WebhookSubscription *struct {
			ID string `json:"id"`
		} `json:"webhookSubscription"`
		UserErrors []MutationUserError `json:"userErrors"`
	} `json:"webhookSubscriptionUpdate"`
}

// WebhookSubscriptionDeleteResponse is WebhookSubscriptionDelete's data shape.
type WebhookSubscriptionDeleteResponse struct {
	WebhookSubscriptionDelete struct {
		DeletedWebhookSubscriptionID *string             `json:"deletedWebhookSubscriptionId"`
		UserErrors                   []MutationUserError `json:"userErrors"`
	} `json:"webhookSubscriptionDelete"`
}
