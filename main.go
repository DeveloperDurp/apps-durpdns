package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/caarlos0/env/v6"
	"github.com/cloudflare/cloudflare-go/v4"
	"github.com/cloudflare/cloudflare-go/v4/dns"
	"github.com/cloudflare/cloudflare-go/v4/option"
	"github.com/joho/godotenv"
	"io"
	"log"
	"net/http"
	"regexp"
	"time"
)

var ipPattern = `^((25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)$`

type WTFIPResponse struct {
	IP          string `json:"YourFuckingIPAddress"`
	Location    string `json:"YourFuckingLocation"`
	Hostname    string `json:"YourFuckingHostname"`
	ISP         string `json:"YourFuckingISP"`
	TorExit     bool   `json:"YourFuckingTorExit"`
	City        string `json:"YourFuckingCity"`
	Country     string `json:"YourFuckingCountry"`
	CountryCode string `json:"YourFuckingCountryCode"`
}

type IpifyResponse struct {
	IP string `json:"ip"`
}

type IpInfoResponse struct {
	IP       string `json:"ip"`
	Hostname string `json:"hostname"`
	City     string `json:"city"`
	Region   string `json:"region"`
	Country  string `json:"country"`
	Loc      string `json:"loc"`
	Org      string `json:"org"`
	Postal   string `json:"postal"`
	Timezone string `json:"timezone"`
	Readme   string `json:"readme"`
}
type Response struct {
	IPAddress string
}

type ResponseInterface interface {
	getIP() Response
}

func (r *WTFIPResponse) getIP() Response {
	return Response{IPAddress: r.IP}
}
func (r *IpInfoResponse) getIP() Response {
	return Response{IPAddress: r.IP}
}
func (r *IpifyResponse) getIP() Response {
	return Response{IPAddress: r.IP}
}

func NewIpifyProvider() *IpProvider {
	return &IpProvider{
		url:  "https://api.ipify.org?format=json",
		resp: new(IpifyResponse),
		client: http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

func NewIpInfoProvider() *IpProvider {
	return &IpProvider{
		url:  "https://ipinfo.io/json",
		resp: new(IpInfoResponse),
		client: http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

func NewWTFProvider() *IpProvider {
	return &IpProvider{
		url:  "https://myip.wtf/json",
		resp: new(WTFIPResponse),
		client: http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

type IpProvider struct {
	url    string
	resp   interface{}
	client http.Client
}

func (p *IpProvider) get() *Response {

	body, err := p.client.Get(p.url)
	if err != nil {
		return nil
	}
	defer body.Body.Close()

	data, err := io.ReadAll(body.Body)
	if err != nil {
		return nil
	}

	err = json.Unmarshal(data, &p.resp)
	if err != nil {
		return nil
	}

	resp := p.resp.(ResponseInterface).getIP()
	pattern := regexp.MustCompile(ipPattern)

	if !pattern.MatchString(resp.IPAddress) {
		return nil
	}

	return &resp
}

type Config struct {
	CloudflareZoneID string `env:"CLOUDFLARE_ZONE_ID"`
	CloudFlareAPIKey string `env:"CLOUDFLARE_API_KEY"`
	CloudflareEmail  string `env:"CLOUDFLARE_EMAIL"`
	IpProviders      []*IpProvider
	Client           *cloudflare.Client
	Timeout          time.Duration
}

func main() {

	err := godotenv.Load(".env")
	if err != nil {
		log.Fatal("Failed to load env file")
		return
	}

	config := &Config{
		Timeout: 5 * time.Second,
	}
	err = env.Parse(config)
	if err != nil {
		log.Fatal("Failed to load env file")
		return
	}

	if config.CloudflareEmail == "" || config.CloudFlareAPIKey == "" || config.CloudflareZoneID == "" {
		log.Fatal("Email, API and ZoneID need to be set")
		return
	}

	config.Client = cloudflare.NewClient(
		option.WithAPIKey(config.CloudFlareAPIKey),
		option.WithAPIEmail(config.CloudflareEmail),
	)

	config.IpProviders = append(
		config.IpProviders,
		NewWTFProvider(),
	)
	config.IpProviders = append(
		config.IpProviders,
		NewIpifyProvider(),
	)
	config.IpProviders = append(
		config.IpProviders,
		NewIpInfoProvider(),
	)

	ExistingIP, err := GetIPAddress(config)

	fmt.Printf("The current IP address is: %s\n", ExistingIP)

	err = updateDNSRecords(
		config,
		ExistingIP,
	)
	if err != nil {
		fmt.Println(err)
	}

do:
	for {
		CurrentIP, err := GetIPAddress(config)
		if err != nil {
			fmt.Println(err)
			continue do
		}

		if ExistingIP == CurrentIP {
			time.Sleep(config.Timeout)
			continue do
		}

		ExistingIP = CurrentIP
		fmt.Printf(
			"Your IP address has changed: %s\n",
			CurrentIP,
		)

		err = updateDNSRecords(
			config,
			CurrentIP,
		)
		if err != nil {
			fmt.Println(err)
		}

		time.Sleep(config.Timeout)
		continue do
	}
}

func GetIPAddress(config *Config) (string, error) {

	responses := []*Response{}

	for _, provider := range config.IpProviders {
		resp := provider.get()
		responses = append(responses, resp)
	}

	// Count the occurrences of each IPAddress
	counts := map[string]int{}
	for _, r := range responses {
		if r != nil {
			IP := r.IPAddress
			if count, ok := counts[IP]; ok {
				counts[IP] = count + 1
			} else {
				counts[IP] = 1
			}
		}
	}

	// Find the IPAddress that repeats the most
	maxCount := 0
	CurrentIP := ""
	for IP, count := range counts {
		if count > maxCount {
			maxCount = count
			CurrentIP = IP
		}
	}

	pattern := regexp.MustCompile(ipPattern)

	if !pattern.MatchString(CurrentIP) {
		return "", errors.New("Did not find a valid IP")
	}

	return CurrentIP, nil
}

func getDNSRecords(
	client *cloudflare.Client,
	zoneID string,
) (*[]dns.RecordResponse, error) {
	dnslist, err := client.DNS.Records.List(
		context.TODO(),
		dns.RecordListParams{
			ZoneID: cloudflare.F(zoneID),
		},
	)
	if err != nil {
		return nil, err
	}

	var records []dns.RecordResponse
	for _, item := range dnslist.Result {

		if item.Type == "A" {
			records = append(records, item)
		}
	}
	return &records, nil
}

func updateDNSRecords(
	config *Config,
	ipAddress string,
) error {
	records, err := getDNSRecords(
		config.Client,
		config.CloudflareZoneID,
	)
	if err != nil {
		return err
	}

	for _, record := range *records {

		if record.Content == ipAddress {
			continue
		}

		NewDNS := dns.ARecordParam{
			Comment: cloudflare.F(record.Comment),
			Content: cloudflare.F(ipAddress),
			Name:    cloudflare.F(record.Name),
			Proxied: cloudflare.F(record.Proxied),
			TTL:     cloudflare.F(record.TTL),
			Type:    cloudflare.F(dns.ARecordTypeA),
		}

		_, err = config.Client.DNS.Records.Edit(
			context.TODO(), record.ID, dns.RecordEditParams{
				ZoneID: cloudflare.F(config.CloudflareZoneID),
				Record: NewDNS,
			},
		)
		if err != nil {
			return err
		}

		fmt.Printf("Updated IP address for %s\n", record.Name)
	}
	return nil
}
