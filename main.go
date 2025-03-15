package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/caarlos0/env/v6"
	"github.com/cloudflare/cloudflare-go/v4"
	"github.com/cloudflare/cloudflare-go/v4/dns"
	"github.com/cloudflare/cloudflare-go/v4/option"
	"github.com/joho/godotenv"
	"io"
	"log"
	"net/http"
	"time"
)

type IPResponse struct {
	IPAddress   string `json:"YourFuckingIPAddress"`
	Location    string `json:"YourFuckingLocation"`
	Hostname    string `json:"YourFuckingHostname"`
	ISP         string `json:"YourFuckingISP"`
	TorExit     bool   `json:"YourFuckingTorExit"`
	City        string `json:"YourFuckingCity"`
	Country     string `json:"YourFuckingCountry"`
	CountryCode string `json:"YourFuckingCountryCode"`
}

type Config struct {
	CloudflareZoneID string `env:"CLOUDFLARE_ZONE_ID"`
	CloudFlareAPIKey string `env:"CLOUDFLARE_API_KEY"`
	CloudflareEmail  string `env:"CLOUDFLARE_EMAIL"`
}

func main() {

	err := godotenv.Load(".env")
	if err != nil {
		log.Fatal("Failed to load env file")
		return
	}

	config := &Config{}
	err = env.Parse(config)
	if err != nil {
		log.Fatal("Failed to load env file")
		return
	}

	if config.CloudflareEmail == "" || config.CloudFlareAPIKey == "" || config.CloudflareZoneID == "" {
		log.Fatal("Email, API and ZoneID need to be set")
		return
	}

	client := cloudflare.NewClient(
		option.WithAPIKey(config.CloudFlareAPIKey),
		option.WithAPIEmail(config.CloudflareEmail),
	)

	ExistingIP, err := getIP()
	if err != nil {
		fmt.Println(err)
		return
	}

	err = updateDNSRecords(
		client,
		config.CloudflareZoneID,
		ExistingIP.IPAddress,
	)
	if err != nil {
		fmt.Println(err)
	}

	fmt.Println("Your public IP address is:", ExistingIP.IPAddress)

do:
	for {
		CurrentIP, err := getIP()
		if err != nil {
			fmt.Println(err)
			continue do
		}

		if ExistingIP.IPAddress != CurrentIP.IPAddress {
			ExistingIP = CurrentIP
			fmt.Println(
				"Your public IP address has updated:",
				ExistingIP.IPAddress,
			)

			err := updateDNSRecords(
				client,
				config.CloudflareZoneID,
				CurrentIP.IPAddress,
			)
			if err != nil {
				fmt.Println(err)
				break
			}

		} else {
			fmt.Println("Your public IP address has not changed.")
		}

		time.Sleep(10 * time.Second)
		continue do
	}
}

func getIP() (*IPResponse, error) {

	client := &http.Client{
		Timeout: time.Second * 5,
	}
	var ip IPResponse
	resp, err := client.Get("https://myip.wtf/json")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(data, &ip)
	return &ip, err
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
	client *cloudflare.Client,
	zoneID string,
	ipAddress string,
) error {
	records, err := getDNSRecords(client, zoneID)
	if err != nil {
		return err
	}

	for _, record := range *records {

		NewDNS := dns.ARecordParam{
			Comment: cloudflare.F(record.Comment),
			Content: cloudflare.F(ipAddress),
			Name:    cloudflare.F(record.Name),
			Proxied: cloudflare.F(record.Proxied),
			TTL:     cloudflare.F(record.TTL),
			Type:    cloudflare.F(dns.ARecordTypeA),
		}

		_, err = client.DNS.Records.Edit(
			context.TODO(), record.ID, dns.RecordEditParams{
				ZoneID: cloudflare.F(zoneID),
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
