package domain

type Product struct {
	ProductId       string   `json:"productId"`
	GroupName       string   `json:"groupName"`
	Vendor          string   `json:"vendor"`
	ListPrice       float64  `json:"listPrice"`
	QuotePrice      float64  `json:"quotePrice"`
	DiscountOptions []string `json:"discountOptions"` // discount option names, in application order
}
