package domain

type Product struct {
	ProductId       string   `json:"productId"`
	Vendor          string   `json:"vendor"`
	ListPrice       float64  `json:"listPrice"`
	DiscountOptions []string `json:"discountOptions"` // discount option names, in application order
}
