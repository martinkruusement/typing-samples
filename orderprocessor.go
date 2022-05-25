package main

import (
	"os"
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/go-redis/redis"
	"html"
	"html/template"
	"log"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"

	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

const sendgrid_api_key string = os.Getenv("SENDGRID_API_KEY")
const orderdb_user string = os.Getenv("ORDERDB_USER")
const orderdb_pass string = os.Getenv("ORDERDB_PASS")
const orderdb_host string = os.Getenv("ORDERDB_HOST")
const orderdb_port string = os.Getenv("ORDERDB_PORT")

const redis_url string = os.Getenv("REDIS_URL")
const redis_pass string = os.Getenv("REDIS_PASS")

const store_name string = os.Getenv("STORE_NAME")
const store_url string = os.Getenv("STORE_URL")
const store_domain string = os.Getenv("STORE_DOMAIN")
const store_email string = os.Getenv("STORE_EMAIL")

var ShippingMethodDisplays map[string]string = map[string]string{
	"dhl-standard": "DHL Standard",
}
var PaymentMethodDisplays map[string]string = map[string]string{
	"invoice": "Rechnung",
}

type Cart struct {
	Products []CartProduct
}

type CartProduct struct {
	ProductID string
	Variants  map[string]uint64
}

var redis_products = redis.NewClient(&redis.Options{
	Addr:     redis_url,
	Password: redis_pass,
	DB:       0,
})
var rds_carts = redis.NewClient(&redis.Options{
	Addr:     redis_url,
	Password: redis_pass,
	DB:       1,
})

type FullCart struct {
	Products map[string]Product
}
type Product struct {
	Brand      string
	Model      string
	Lamptype   string
	Picture    string
	BrandID    string
	ModelID    string
	LamptypeID string
	Variants   map[string]*ProductVariant
}
type ProductVariant struct {
	//	LineItemID  uint64
	ProductTier          string
	ProductTierDisplay   string
	Price                float64
	PriceDisplay         string
	OriginalPrice        float64
	OriginalPriceDisplay string
	//	Stock       uint64
	//	LeadTime    uint64
	DeliveryTime string
	//	Supplier    string
	//	SupplierID  string
	Quantity uint64
}

func (variant *ProductVariant) UpdateQuantity(quantity uint64) uint64 {
	variant.Quantity = quantity
	return quantity
}

type OrderForm struct {
	BillingTitle        string `bson:"BillingTitle"`
	BillingName         string `bson:"BillingName"`
	BillingEmail        string `bson:"BillingEmail"`
	BillingPhone        string `bson:"BillingPhone"`
	BillingOrganization string `bson:"BillingOrganization"`
	BillingStreet       string `bson:"BillingStreet"`
	BillingStreet2      string `bson:"BillingStreet2"`
	BillingZip          string `bson:"BillingZip"`
	BillingCity         string `bson:"BillingCity"`

	ShippingTitle        string `bson:"ShippingTitle"`
	ShippingName         string `bson:"ShippingName"`
	ShippingOrganization string `bson:"ShippingOrganization"`
	ShippingStreet       string `bson:"ShippingStreet"`
	ShippingStreet2      string `bson:"ShippingStreet2"`
	ShippingZip          string `bson:"ShippingZip"`
	ShippingCity         string `bson:"ShippingCity"`

	OptionsPaymentMethod         string `bson:"OptionsPaymentMethod"`
	OptionsShippingMethod        string `bson:"OptionsShippingMethod"`
	OptionsSignup                string `bson:"OptionsSignup"`
	OptionsReference             string `bson:"OptionsReference"`
	OptionsUseBillingForShipping bool   `bson:"OptionsUseBillingForShipping"`
	OptionsCartID                string `bson:"OptionsCartID"`

	ShippingMethodDisplay string `bson:"ShippingMethodDisplay"`
	PaymentMethodDisplay  string `bson:"PaymentMethodDisplay"`

	Shipping   string `bson:"Shipping"`
	Tax        string `bson:"Tax"`
	GrandTotal string `bson:"GrandTotal"`

	Customer_HTML_productlist template.HTML `bson:"Customer_HTML_productlist"`
	Customer_TXT_productlist  string        `bson:"Customer_TXT_productlist"`

	Customer_TY_firstline string `bson:"Customer_TY_firstline"`
}

func (form *OrderForm) UpdateCartTotals(shipping float64, grandtotal float64, tax float64) {
	form.Shipping = format_price(shipping)
	form.Tax = format_price(tax)
	form.GrandTotal = format_price(grandtotal)
}

func (form *OrderForm) UpdateCartEmailTablesCustomer(_html bytes.Buffer, _txt bytes.Buffer) {
	form.Customer_HTML_productlist = template.HTML(_html.String())
	form.Customer_TXT_productlist = _txt.String()
}
func (form *OrderForm) UpdateCartEmailCustomisations() {
	if form.BillingTitle == "Herr" {
		form.Customer_TY_firstline = "Sehr geehrter Herr"
	}
	if form.BillingTitle == "Frau" {
		form.Customer_TY_firstline = "Sehr geehrte Frau"
	}
}

type EmailTemplates struct {
	ThankYouCustomerHTML    *template.Template
	ThankYouCustomerTXT     *template.Template
	ThankYouCustomerRowHTML *template.Template
	ThankYouCustomerRowTXT  *template.Template
}
type ProductLineItem struct {
	SortID               string  `bson:"SortID"`
	Brand                string  `bson:"Brand"`
	BrandID              string  `bson:"BrandID"`
	Model                string  `bson:"Model"`
	ModelID              string  `bson:"ModelID"`
	Quantity             uint64  `bson:"Quantity"`
	ProductTier          string  `bson:"ProductTier"`
	ProductTierDisplay   string  `bson:"ProductTierDisplay"`
	Price                float64 `bson:"Price"`
	PriceDisplay         string  `bson:"PriceDisplay"`
	OriginalPrice        float64 `bson:"OriginalPrice"`
	OriginalPriceDisplay string  `bson:"OriginalPriceDisplay"`
	Picture              string  `bson:"Picture"`
	TotalPrice           float64 `bson:"TotalPrice"`
	TotalPriceDisplay    string  `bson:"TotalPriceDisplay"`
	DeliveryTime         string  `bson:"DeliveryTime"`
}

type OrderDocument struct {
	Customer OrderForm         `bson:"Customer"`
	Products []ProductLineItem `bson:"Products"`
	Order    struct {
		OrderNumber int64     `bson:"OrderNumber"`
		Total       float64   `bson:"Total"`
		Tax         float64   `bson:"Tax"`
		Shipping    float64   `bson:"Shipping"`
		Date        time.Time `bson:"Date"`
		Status      string    `bson:"Status"`
		Shop        string    `bson:"Shop"`
	} `bson:"Order"`
	Analytics struct {
		MixpanelID string `bson:"MixpanelID"`
		AdwordsKW  string `bson:"AdwordsKW"`
		IP         string `bson:"IP"`
	} `bson:"Analytics"`
}

type OrderNr struct {
	Order struct {
		OrderNumber int64 `bson:"OrderNumber"`
	} `bson:"Order"`
}

var email_templates EmailTemplates

func load_email_templates() {
	ThankYouCustomerHTML, templ_error := template.ParseFiles("emailtemplates/ty_customer.html")
	if templ_error != nil {
		fmt.Println("[TEMPLATE ERROR]", templ_error)
	}
	ThankYouCustomerTXT, templ_error := template.ParseFiles("emailtemplates/ty_customer.txt")
	if templ_error != nil {
		fmt.Println("[TEMPLATE ERROR]", templ_error)
	}
	ThankYouCustomerRowHTML, templ_error := template.ParseFiles("emailtemplates/ty_customer_lineitem.html")
	if templ_error != nil {
		fmt.Println("[TEMPLATE ERROR]", templ_error)
	}
	ThankYouCustomerRowTXT, templ_error := template.ParseFiles("emailtemplates/ty_customer_lineitem.txt")
	if templ_error != nil {
		fmt.Println("[TEMPLATE ERROR]", templ_error)
	}

	email_templates = EmailTemplates{
		ThankYouCustomerHTML:    ThankYouCustomerHTML,
		ThankYouCustomerTXT:     ThankYouCustomerTXT,
		ThankYouCustomerRowHTML: ThankYouCustomerRowHTML,
		ThankYouCustomerRowTXT:  ThankYouCustomerRowTXT,
	}

}

func send_email(
	sender_name string,
	sender_email string,
	subject string,
	recipient_name string,
	recipient_email string,
	html bytes.Buffer,
	txt bytes.Buffer,
) {
	sender := mail.NewEmail(sender_name, sender_email)
	recipient := mail.NewEmail(recipient_name, recipient_email)
	message := mail.NewSingleEmail(sender, subject, recipient, txt.String(), html.String())
	sg_client := sendgrid.NewSendClient(sendgrid_api_key)

	_ = sender
	_ = recipient
	_ = message
	_ = sg_client

	response, err := sg_client.Send(message)
	if err != nil {
		log.Println(err)
	} else {
		fmt.Println(response.StatusCode)
		fmt.Println(response.Body)
		fmt.Println(response.Headers)
	}

}

func serve_process_order(browser http.ResponseWriter, request *http.Request) {
	browser.Header().Set("Access-Control-Allow-Origin", store_url)
	browser.Header().Set("Access-Control-Allow-Methods", "POST")
	browser.Header().Set("Access-Control-Allow-Headers", "Content-Type,Content-Length")
	browser.Header().Set("Content-Type", "application/json")

	OptionsUseBillingForShipping, _ := strconv.ParseBool(request.FormValue("OptionsUseBillingForShipping"))

	var form OrderForm = OrderForm{
		BillingTitle:                 request.FormValue("BillingTitle"),
		BillingName:                  request.FormValue("BillingName"),
		BillingEmail:                 request.FormValue("BillingEmail"),
		BillingPhone:                 request.FormValue("BillingPhone"),
		BillingOrganization:          request.FormValue("BillingOrganization"),
		BillingStreet:                request.FormValue("BillingStreet"),
		BillingStreet2:               request.FormValue("BillingStreet2"),
		BillingZip:                   request.FormValue("BillingZip"),
		BillingCity:                  request.FormValue("BillingCity"),
		ShippingTitle:                request.FormValue("ShippingTitle"),
		ShippingName:                 request.FormValue("ShippingName"),
		ShippingOrganization:         request.FormValue("ShippingOrganization"),
		ShippingStreet:               request.FormValue("ShippingStreet"),
		ShippingStreet2:              request.FormValue("ShippingStreet2"),
		ShippingZip:                  request.FormValue("ShippingZip"),
		ShippingCity:                 request.FormValue("ShippingCity"),
		OptionsPaymentMethod:         request.FormValue("OptionsPaymentMethod"),
		OptionsShippingMethod:        request.FormValue("OptionsShippingMethod"),
		OptionsSignup:                request.FormValue("OptionsSignup"),
		OptionsReference:             request.FormValue("OptionsReference"),
		OptionsUseBillingForShipping: OptionsUseBillingForShipping,
		OptionsCartID:                request.FormValue("OptionsCartID"),
		ShippingMethodDisplay:        ShippingMethodDisplays[request.FormValue("OptionsShippingMethod")],
		PaymentMethodDisplay:         PaymentMethodDisplays[request.FormValue("OptionsPaymentMethod")],
	}

	fmt.Println("-----NEW ORDER---------")
	fmt.Println("BillingTitle:", form.BillingTitle)
	fmt.Println("BillingName:", form.BillingName)
	fmt.Println("BillingEmail:", form.BillingEmail)
	fmt.Println("BillingPhone:", form.BillingPhone)
	fmt.Println("BillingOrganization:", form.BillingOrganization)
	fmt.Println("BillingStreet:", form.BillingStreet)
	fmt.Println("BillingStreet2:", form.BillingStreet2)
	fmt.Println("BillingZip:", form.BillingZip)
	fmt.Println("BillingCity:", form.BillingCity)
	fmt.Println("ShippingTitle:", form.ShippingTitle)
	fmt.Println("ShippingName:", form.ShippingName)
	fmt.Println("ShippingOrganization:", form.ShippingOrganization)
	fmt.Println("ShippingStreet:", form.ShippingStreet)
	fmt.Println("ShippingStreet2:", form.ShippingStreet2)
	fmt.Println("ShippingZip:", form.ShippingZip)
	fmt.Println("ShippingCity:", form.ShippingCity)
	fmt.Println("OptionsPaymentMethod:", form.OptionsPaymentMethod)
	fmt.Println("OptionsShippingMethod:", form.OptionsShippingMethod)
	fmt.Println("OptionsSignup:", form.OptionsSignup)
	fmt.Println("OptionsReference:", form.OptionsReference)
	fmt.Println("OptionsUseBillingForShipping:", form.OptionsUseBillingForShipping)
	fmt.Println("OptionsCartID:", form.OptionsCartID)
	fmt.Println("-----------------------")

	var cart_key = fmt.Sprintf("CARTS__%s", form.OptionsCartID)

	raw_cart, err_cart_notfound := rds_carts.Get(cart_key).Bytes()

	if err_cart_notfound != nil {
		fmt.Println("[CART NOT FOUND]", request.FormValue("cart_id"), cart_key, err_cart_notfound)
		browser_reply, _ := json.Marshal(struct {
			Result  string
			Details string
		}{"error", "cart_not_found"})
		browser.Write(browser_reply)
		return
	}

	var compact_cart Cart = Cart{}
	json_error := json.Unmarshal(raw_cart, &compact_cart)

	if json_error != nil {
		fmt.Println("[ERROR] Can't convert cart to json:", json_error)
		// TODO: Report incident, maybe can be recovered manually by the customer
		browser_reply, _ := json.Marshal(struct {
			Result  string
			Details string
		}{"error", "cant_convert_cart"})
		browser.Write(browser_reply)
	}

	var full_cart FullCart = build_full_cart(compact_cart)
	var cart_lineitems []ProductLineItem = list_lineitems(full_cart)

	var Shipping float64 = 0.00
	var GrandTotal float64 = 0
	var Tax float64 = 0

	for _, cart_item := range cart_lineitems {
		GrandTotal += cart_item.Price * float64(cart_item.Quantity)
	}
	Tax = (GrandTotal + Tax) * 0.19
	form.UpdateCartTotals(Shipping, GrandTotal, Tax)

	// TODO: CHECK IF THERE IS EVEN ANYTHING IN THE CART
	// - error if not

	ip, _, _ := net.SplitHostPort(request.RemoteAddr)
	user_ip := net.ParseIP(ip)

	var order_document = OrderDocument{}
	order_document.Customer = form
	order_document.Products = cart_lineitems

	order_document.Order.OrderNumber = 0
	order_document.Order.Total = GrandTotal
	order_document.Order.Tax = Tax
	order_document.Order.Shipping = Shipping
	order_document.Order.Date = time.Now()
	order_document.Order.Status = "unprocessed"
	order_document.Order.Shop = store_domain

	order_document.Analytics.MixpanelID = "-"
	order_document.Analytics.AdwordsKW = "-"
	order_document.Analytics.IP = user_ip.String()

	dbsave_success, order_document := save_order_db(order_document)
	_ = dbsave_success

	rds_carts.Set(cart_key, "\"{'Products':[]}\"", 0)

	var emails_sent bool = process_order_confirmation_emails(order_document)
	_ = emails_sent

	browser_reply, _ := json.Marshal(struct {
		Result  string
		Details string
	}{"success", fmt.Sprintf("%v", order_document.Order.OrderNumber)})
	browser.Write(browser_reply)
}

func save_order_db(order_document OrderDocument) (bool, OrderDocument) {
	mongo, _ := mgo.Dial(fmt.Sprintf("%s:%s", orderdb_host, orderdb_port))
	defer mongo.Close()

	_ = mongo.DB("Orders").Login(orderdb_user, orderdb_pass)
	orders := mongo.DB("Orders").C("orders")

	var order_number int64 = get_next_unused_order_nr(orders)
	fmt.Println("New order number:", order_number)
	order_document.Order.OrderNumber = order_number

	insert_error := orders.Insert(order_document)
	if insert_error != nil {
		fmt.Println("DB INSERT ERROR:", insert_error)
		return false, order_document
	}

	return true, order_document
}

func process_order_confirmation_emails(order_document OrderDocument) bool {
	var succeeded bool = true
	order_document, ty_customer_html, ty_customer_txt := build_order_confirmation_emails_customer(order_document)
	order_document, ty_internal_html, ty_internal_txt := build_order_confirmation_emails_internal(order_document)

	// Martin
	send_email(
		store_name,
		store_email,
		fmt.Sprintf("[#%v] New German lamps order.", order_document.Order.OrderNumber),
		"Martin Kruusement",
		"martin@limitlessprojects.com",
		ty_customer_html,
		ty_customer_txt,
	)

	// Customer
	send_email(
		store_name,
		store_email,
		"Vielen Dank für Ihre Bestellung.",
		order_document.Customer.BillingName,
		order_document.Customer.BillingEmail,
		ty_internal_html,
		ty_internal_txt,
	)

	return succeeded
}

func build_order_confirmation_emails_customer(order_document OrderDocument) (OrderDocument, bytes.Buffer, bytes.Buffer) {
	var html bytes.Buffer
	var txt bytes.Buffer
	var html_productlist bytes.Buffer
	var txt_productlist bytes.Buffer

	var cart_item ProductLineItem
	for i := 0; i < len(order_document.Products); i++ {
		cart_item = order_document.Products[i]
		email_templates.ThankYouCustomerRowHTML.Execute(&html_productlist, cart_item)
		email_templates.ThankYouCustomerRowTXT.Execute(&txt_productlist, cart_item)
	}

	order_document.Customer.UpdateCartEmailTablesCustomer(
		html_productlist,
		txt_productlist,
	)

	order_document.Customer.UpdateCartEmailCustomisations()

	email_templates.ThankYouCustomerHTML.Execute(&html, order_document)
	email_templates.ThankYouCustomerTXT.Execute(&txt, order_document)

	return order_document, html, txt
}

func build_full_cart(compact_cart Cart) FullCart {
	// Build cart data:
	var full_cart FullCart = FullCart{Products: make(map[string]Product)}
	for _, cart_item := range compact_cart.Products {
		productID_parts := strings.Split(cart_item.ProductID, "/")
		product_key := fmt.Sprintf("PRODUCT_MODEL__%s__%s", productID_parts[0], productID_parts[1])
		raw_product, err_product_notfound := redis_products.Get(product_key).Bytes()
		if err_product_notfound != nil {
			fmt.Println("[PRODUCT NOT FOUND]", err_product_notfound)
			// TODO: Remove it from the cart
		} else {
			// Product found:
			var current_product Product
			json_error := json.Unmarshal(raw_product, &current_product)
			if json_error != nil {
				fmt.Println("[PRODUCT PARSE ERROR]", json_error)
			} else {
				full_cart.Products[cart_item.ProductID] = current_product
				for variant_key, variant := range full_cart.Products[cart_item.ProductID].Variants {
					//fmt.Println("Parse variant:", variant_key, variant)
					_ = variant
					if cart_item.Variants[variant_key] != 0 {
						full_cart.Products[cart_item.ProductID].Variants[variant_key].Quantity = cart_item.Variants[variant_key]
						full_cart.Products[cart_item.ProductID].Variants[variant_key].PriceDisplay = format_price(full_cart.Products[cart_item.ProductID].Variants[variant_key].Price)
						full_cart.Products[cart_item.ProductID].Variants[variant_key].OriginalPriceDisplay = format_price(full_cart.Products[cart_item.ProductID].Variants[variant_key].OriginalPrice)
						full_cart.Products[cart_item.ProductID].Variants[variant_key].ProductTierDisplay = format_product_tiers(variant_key)
					}
					if cart_item.Variants[variant_key] == 0 {
						fmt.Println("Quantity == 0")
						delete(full_cart.Products[cart_item.ProductID].Variants, variant_key)
						// TODO: If now all are 0, remove from cart
					}
				}
			}
		}
		//fmt.Println("cart item:", product_key)
	}
	return full_cart
}

func build_order_confirmation_emails_internal(order_document OrderDocument) (OrderDocument, bytes.Buffer, bytes.Buffer) {
	var html bytes.Buffer
	var txt bytes.Buffer
	var html_productlist bytes.Buffer
	var txt_productlist bytes.Buffer

	var cart_item ProductLineItem
	for i := 0; i < len(order_document.Products); i++ {
		cart_item = order_document.Products[i]
		email_templates.ThankYouCustomerRowHTML.Execute(&html_productlist, cart_item)
		email_templates.ThankYouCustomerRowTXT.Execute(&txt_productlist, cart_item)
	}

	order_document.Customer.UpdateCartEmailTablesCustomer(
		html_productlist,
		txt_productlist,
	)

	email_templates.ThankYouCustomerHTML.Execute(&html, order_document)
	email_templates.ThankYouCustomerTXT.Execute(&txt, order_document)

	return order_document, html, txt
}

func list_lineitems(cart_data FullCart) []ProductLineItem {
	var cart_lineitems []ProductLineItem
	for _, product_item := range cart_data.Products {
		for variant_id, variant_item := range product_item.Variants {
			cart_lineitems = append(cart_lineitems, ProductLineItem{
				SortID:               fmt.Sprintf("%s/%s/%s", product_item.BrandID, product_item.ModelID, variant_id),
				Brand:                product_item.Brand,
				BrandID:              product_item.BrandID,
				Model:                product_item.Model,
				ModelID:              product_item.ModelID,
				Quantity:             variant_item.Quantity,
				ProductTier:          variant_id,
				ProductTierDisplay:   variant_item.ProductTierDisplay,
				Price:                variant_item.Price,
				PriceDisplay:         variant_item.PriceDisplay,
				OriginalPrice:        variant_item.OriginalPrice,
				OriginalPriceDisplay: variant_item.OriginalPriceDisplay,
				Picture:              product_item.Picture,
				TotalPrice:           variant_item.Price * float64(variant_item.Quantity),
				TotalPriceDisplay:    format_price(variant_item.Price * float64(variant_item.Quantity)),
			})
		}
	}

	// Sort by Brand/Model/Variant one by one (not a good idea here)
	//	sort.SliceStable(cart_lineitems, func(i, j int) bool { return cart_lineitems[i].BrandID < cart_lineitems[j].BrandID })
	//	sort.SliceStable(cart_lineitems, func(i, j int) bool { return cart_lineitems[i].ModelID < cart_lineitems[j].ModelID })
	//	sort.SliceStable(cart_lineitems, func(i, j int) bool { return cart_lineitems[i].ProductTier < cart_lineitems[j].ProductTier })
	sort.SliceStable(cart_lineitems, func(i, j int) bool { return cart_lineitems[i].SortID < cart_lineitems[j].SortID })

	return cart_lineitems
}

func format_price(price float64) string {
	s_price := strconv.FormatFloat(price, 'f', 2, 64)
	return strings.Replace(s_price, ".", ",", 1)
}
func format_product_tiers(tier string) string {
	if tier == "professional" {
		return "Professional Line"
	}
	fmt.Println("[PRODUCT TIER NOT FOUND]", tier)
	return "Not Found"
}

func get_next_unused_order_nr(orders *mgo.Collection) int64 {
	latest_order := OrderNr{}
	_ = orders.Find(nil).Select(bson.M{"Order.OrderNumber": 1}).Sort("-Order.OrderNumber").Limit(1).One(&latest_order)
	fmt.Println("found highest:", latest_order.Order.OrderNumber)

	var last_order_number int64 = latest_order.Order.OrderNumber
	var new_order_number int64 = last_order_number + 1

	return new_order_number
}

func main() {
	_ = html.UnescapeString("<")
	fmt.Println("Starting order-processing server on Port 8011...")
	load_email_templates()
	http.HandleFunc("/order/process", serve_process_order)
	server_error := http.ListenAndServeTLS(":8011",
		"/etc/apache2/cert/combined.crt",
		"/etc/apache2/cert/key.pem",
		nil)

	fmt.Println("[ERROR] Can't start server:", server_error)
}
