package productcontract

import "context"

type PurchaseChecker interface {
	HasPurchasedProduct(ctx context.Context, projectID string, userID uint64, productID string) (purchased bool, err error)
}
