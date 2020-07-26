package main

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws/external"
	"github.com/aws/aws-sdk-go-v2/service/sns"
)

var (
	searchString = "Sökningen gav inga träffar"
	snsSubject   = "Nytt förråd!"
	snsMessage   = "Det verkar finnas ett nytt förråd tillgängligt!\n\nGå till https://www.stockholmshem.se/mina-sidor/smaforrad/ för att kontrollera"
	headers      = map[string]string{
		"User-Agent":   "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/84.0.4147.89 Safari/537.36",
		"Accept":       "*/*",
		"Content-Type": "application/x-www-form-urlencoded",
	}
)

func main() {
	lambda.Start(handler)
}

func handler(ctx context.Context) error {
	// Create the http client.
	client, err := createHTTPClient(10000)
	if err != nil {
		return err
	}

	// Login against stockholmshem.
	if err := login(client, headers); err != nil {
		return err
	}

	// Check if there are any förråd.
	new, err := forrad(client, headers)
	if err != nil {
		return err
	}

	// Either send a message about new förråd or just return nil.
	return send(ctx, new)
}

// createHTTPClient will create an new http client with a cookie jar with timeout in milliseconds.
// Returns *http.Client and error.
func createHTTPClient(timeout int) (*http.Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("couldn't create cookie jar. %s", err.Error())
	}

	return &http.Client{
		Jar:     jar,
		Timeout: time.Millisecond * time.Duration(timeout),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, nil
}

// crateHTTPRequest will create a request using method, url and payload and set headers based on headers.
// Returns *http.Request and error.
func createHTTPRequest(method string, url string, payload []byte, headers map[string]string) (*http.Request, error) {
	// Replace any {epoch} with now.unix * 1000.
	now := time.Now().Unix() * 1000
	url = strings.ReplaceAll(url, "{epoch}", fmt.Sprintf("%d", now))

	req, err := http.NewRequest(method, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("couldn't create request for %s %s. %s", method, url, err.Error())
	}

	// Set headers.
	for key, val := range headers {
		req.Header.Set(key, val)
	}

	return req, nil
}

// sendHTTPRequest will send req using client and save any cookies
// to the clients jar and return the body. If the response status code
// doesn't match statusCode an error will be returned instead.
// Returns []byte and error.
func sendHTTPRequest(client *http.Client, req *http.Request, statusCode int) ([]byte, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("couldn't send http request. %s", err.Error())
	}
	defer resp.Body.Close()

	// Read the body.
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("couldn't read body of response. %s", err.Error())
	}

	// Check that statuscode matches what we expect.
	// But also return the body.
	if resp.StatusCode != statusCode {
		return body, fmt.Errorf("status code missmatch. wanted %d got %d for %s", statusCode, resp.StatusCode, resp.Request.URL.String())
	}

	// Save all cookies if response is successfull.
	client.Jar.SetCookies(resp.Request.URL, resp.Cookies())
	return body, nil
}

// login will login against stockholms hem.
// Returns error.
func login(client *http.Client, headers map[string]string) error {
	// Create payload.
	payload, err := createLoginPayload()
	if err != nil {
		return err
	}

	// Create the request.
	req, err := createHTTPRequest("POST", "https://www.stockholmshem.se/logga-in/?returnUrl=/mina-sidor/smaforrad/", payload, headers)
	if err != nil {
		return err
	}

	// Send the request.
	_, err = sendHTTPRequest(client, req, 302)
	if err != nil {
		return err
	}

	return nil
}

// CreateLoginPayload returns payload that can be used for login or error
// if it can't be created.
// Returns []byte and error.
func createLoginPayload() ([]byte, error) {
	user, ok := os.LookupEnv("PERSONNR")
	if !ok {
		return nil, fmt.Errorf("couldn't get PERSONNR from environment")
	}
	pass, ok := os.LookupEnv("PASSWORD")
	if !ok {
		return nil, fmt.Errorf("couldn't get PASSWORD from environment")
	}

	return []byte(fmt.Sprintf("Username=%s&Password=%s", user, pass)), nil
}

// forrad will check if there are any förråd avaible. Returns true if this is the case.
// Returns bool and error.
func forrad(client *http.Client, headers map[string]string) (bool, error) {
	// Create the request.
	req, err := createHTTPRequest("GET", "https://www.stockholmshem.se/widgets/?callback=jQuery17105048823634686723_{epoch}&widgets%5B%5D=alert&widgets%5B%5D=objektlista%40forrad&_={epoch}", nil, headers)
	if err != nil {
		return false, err
	}

	// Send the request.
	body, err := sendHTTPRequest(client, req, 200)
	if err != nil {
		return false, err
	}

	// Search string.
	return !strings.Contains(string(body), searchString), nil
}

// send will send a message to the configured sns topic if there is a new förråd.
// Returns error.
func send(ctx context.Context, new bool) error {
	// If there are no new just return nil.
	if !new {
		return nil
	}

	// Simple log that we there are new förråd.
	log.Printf("New förråd detected!\n")

	topic, ok := os.LookupEnv("TOPIC")
	if !ok {
		return fmt.Errorf("couldn't get TOPIC from environment")
	}

	// Configure AWS config.
	cfg, err := external.LoadDefaultAWSConfig()
	if err != nil {
		return fmt.Errorf("couldn't load AWS config. %s", err.Error())
	}

	// Configure SNS.
	svc := sns.New(cfg)

	// Send the message.
	_, err = svc.PublishRequest(&sns.PublishInput{
		Subject:  &snsSubject,
		Message:  &snsMessage,
		TopicArn: &topic,
	}).Send(ctx)
	if err != nil {
		return fmt.Errorf("couldn't publish to sns. %s", err.Error())
	}

	return nil
}
