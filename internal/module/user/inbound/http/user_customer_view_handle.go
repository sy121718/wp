package userhttp

import (
	userdto "go_wp/internal/module/user/dto"
	"go_wp/pkg/pagination"
)

type CohortColumnResp struct {
	Index int
	Text  string
}

type CustomerCohortPageResp struct {
	Title string
	Path  string
	Range any

	ErrKey      string
	ErrFallback string
	HasErr      bool

	CohortReady bool

	Columns []CohortColumnResp
	Colspan int

	Cohorts   int64
	Customers int64
	Months    int
	Rows      []any

	GrowthReady         bool
	OrderingCustomers   int64
	NewCustomers        int64
	ReturningCustomers  int64
	Repurchasers        int64
	NewRepurchasers     int64
	RepurchaseRateLabel string

	Pagination pagination.Page
}

type CounterTabResp struct {
	Key string

	LabelKey string
	Label    string
	Count    int64
	Active   bool
	URL      string
}

type RfmSegmentTabResp struct {
	Key string

	LabelKey string
	Label    string
	Count    int64
	Active   bool
	URL      string
}

type CustomerRfmPageResp struct {
	Title   string
	Path    string
	Range   any
	Segment string

	ErrKey      string
	ErrFallback string
	HasErr      bool

	Rows  []any
	Page  int
	Limit int
	Total int64

	RfmReady bool

	SegmentTabs []RfmSegmentTabResp

	Customers int64
	Vip       int64
	Potential int64
	LowValue  int64

	Pagination pagination.Page
}

type MembershipTierOptionResp struct {
	ID   string
	Name string
}

type CustomerListRowResp struct {
	ID       uint64
	Username string
	Email    string

	DisplayName string
	Nickname    string

	Status                int
	StatusLabelKey        string
	StatusLabelFallback   string
	EmailVerified         bool
	EmailVerifiedKey      string
	EmailVerifiedFallback string

	RegisteredAt      any
	RegisterIP        string
	RegisterLocation  string
	LastLoginTime     any
	LastLoginIP       string
	LastLoginLocation string
	Locked            bool
	LockedUntil       any
	LoginFailureCount int

	Actionable        bool
	NextStatus        int
	StatusActionKey   string
	StatusActionLabel string

	PendingHintKey      string
	PendingHintFallback string
	LockHintKey         string
	LockHintFallback    string
}

type CustomersListPageResp struct {
	Title    string
	Menu     string
	Rows     []CustomerListRowResp
	Total    int64
	Page     int
	Limit    int
	Counters userdto.CustomerCounters

	CounterTabs []CounterTabResp

	TierOptions []MembershipTierOptionResp

	FilterActive      bool
	FilterStatus      int
	FilterVerified    int
	FilterLocked      bool
	FilterTier        string
	FilterMinOrders   string
	FilterSegment     string
	FilterRfm         string
	FilterSegmentFrom string
	FilterSegmentTo   string
	FilterFrom        string
	FilterTo          string
	FilterKeyword     string

	ErrKey      string
	ErrFallback string
	HasErr      bool

	CapabilityMissing bool
	BackPath          string

	Pagination pagination.Page
}

type CustomerDetailPageResp struct {
	Title string `view:"title"`
	Menu  string `view:"menu"`

	ErrKey      string
	ErrFallback string
	HasErr      bool

	HasDetail bool

	Customer any

	Projects        []any
	SelectedProject string
	HasProjects     bool
	ProjectsFailed  bool
	SummaryFailed   bool
	HasSummary      bool

	ListURL string

	ID                    uint64
	Username              string
	Email                 string
	DisplayName           string
	Nickname              string
	Status                int
	StatusLabelKey        string
	StatusLabelFallback   string
	EmailVerified         bool
	EmailVerifiedKey      string
	EmailVerifiedFallback string
	RegisteredAt          any
	RegisterIP            string
	RegisterLocation      string
	LastLoginTime         any
	LastLoginIP           string
	LastLoginLocation     string
	Locked                bool
	LockedUntil           any
	LoginFailureCount     int
	Actionable            bool
	NextStatus            int
	PendingHintKey        string
	PendingHintFallback   string
	LockHintKey           string
	LockHintFallback      string
	StatusActionKey       string
	StatusActionLabel     string

	OrderCount              int64
	PaidOrderCount          int64
	TotalAmountLabel        string
	LastOrderNo             string
	LastOrderTime           any
	LastOrderID             uint64
	HasOrders               bool
	LastOrderStatusKey      string
	LastOrderStatusFallback string

	HasMembership             bool
	MembershipTierName        string
	MembershipIsDefault       bool
	MembershipFreeShipping    bool
	MembershipDiscountPercent any
	HasMembershipNotice       bool
	MembershipNoticeKey       string
	MembershipNoticeFallback  string
}
