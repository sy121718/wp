package maildto

type TrackPayload struct {
	LogID      uint64
	CampaignID uint64
	ContactID  uint64

	URL string
}
