package domain

type Product struct {
	VendorCode      string   `json:"vendorCode,omitempty"`
	ProductId       string   `json:"productId"`
	GroupName       string   `json:"groupName"`
	Vendor          string   `json:"vendor"`
	ListPrice       float64  `json:"listPrice"`
	QuotePrice      float64  `json:"quotePrice"`
	DiscountOptions []string `json:"discountOptions"` // discount option names, in application order
}
