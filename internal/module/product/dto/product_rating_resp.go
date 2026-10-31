package productdto

import "go_wp/pkg/utils"

type RatingItemResp struct {
	ID        string         `json:"id"`
	ProductID string         `json:"productId"`
	Score     float64        `json:"score"`
	Source    string         `json:"source"`
	CreatedAt utils.JSONTime `json:"createdAt"`
}

type RatingResp struct {
	Items []RatingItemResp `json:"items"`

	Rating      float64 `json:"rating"`
	RatingCount int     `json:"ratingCount"`
	HasRating   bool    `json:"hasRating"`
}
