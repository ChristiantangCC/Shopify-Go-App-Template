package graphql

// ActiveSubscriptions reads the app's current subscription on a shop.
const ActiveSubscriptions = `query TemplateActiveSubscriptions {
  currentAppInstallation {
    activeSubscriptions {
      id
      name
      status
      test
      currentPeriodEnd
    }
  }
}`

// ActiveSubscriptionsResponse is ActiveSubscriptions' data shape.
type ActiveSubscriptionsResponse struct {
	CurrentAppInstallation struct {
		ActiveSubscriptions []struct {
			ID               string `json:"id"`
			Name             string `json:"name"`
			Status           string `json:"status"`
			Test             bool   `json:"test"`
			CurrentPeriodEnd string `json:"currentPeriodEnd"`
		} `json:"activeSubscriptions"`
	} `json:"currentAppInstallation"`
}

// AppSubscriptionCreate starts a recurring app subscription and returns the
// confirmation URL the merchant must approve. Only needed by apps that
// cannot use managed pricing.
const AppSubscriptionCreate = `mutation TemplateAppSubscriptionCreate($name: String!, $returnUrl: URL!, $test: Boolean, $lineItems: [AppSubscriptionLineItemInput!]!) {
  appSubscriptionCreate(name: $name, returnUrl: $returnUrl, test: $test, lineItems: $lineItems) {
    confirmationUrl
    appSubscription {
      id
    }
    userErrors {
      field
      message
    }
  }
}`

// AppSubscriptionCreateResponse is AppSubscriptionCreate's data shape.
type AppSubscriptionCreateResponse struct {
	AppSubscriptionCreate struct {
		ConfirmationURL *string `json:"confirmationUrl"`
		AppSubscription *struct {
			ID string `json:"id"`
		} `json:"appSubscription"`
		UserErrors []MutationUserError `json:"userErrors"`
	} `json:"appSubscriptionCreate"`
}
