package ews

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const soapEnvelopeTemplate = `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"
               xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types"
               xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages">
  <soap:Header>
    <t:RequestServerVersion Version="Exchange2010_SP2"/>
  </soap:Header>
  <soap:Body>
    <m:ResolveNames ReturnFullContactData="true" SearchScope="ActiveDirectory">
      <m:UnresolvedEntry>%s</m:UnresolvedEntry>
    </m:ResolveNames>
  </soap:Body>
</soap:Envelope>`

// SOAP response structures

type envelope struct {
	Body body `xml:"Body"`
}

type body struct {
	ResolveNamesResponse resolveNamesResponse `xml:"ResolveNamesResponse"`
}

type resolveNamesResponse struct {
	ResponseMessages responseMessages `xml:"ResponseMessages"`
}

type responseMessages struct {
	ResolveNamesResponseMessage resolveNamesResponseMessage `xml:"ResolveNamesResponseMessage"`
}

type resolveNamesResponseMessage struct {
	ResponseClass string        `xml:"ResponseClass,attr"`
	ResponseCode  string        `xml:"ResponseCode"`
	ResolutionSet resolutionSet `xml:"ResolutionSet"`
}

type resolutionSet struct {
	TotalItemsInView        int          `xml:"TotalItemsInView,attr"`
	IncludesLastItemInRange string       `xml:"IncludesLastItemInRange,attr"`
	Resolutions             []resolution `xml:"Resolution"`
}

type resolution struct {
	Mailbox mailbox `xml:"Mailbox"`
	Contact contact `xml:"Contact"`
}

type mailbox struct {
	Name         string `xml:"Name"`
	EmailAddress string `xml:"EmailAddress"`
	RoutingType  string `xml:"RoutingType"`
	MailboxType  string `xml:"MailboxType"`
}

type contact struct {
	DisplayName    string       `xml:"DisplayName"`
	GivenName      string       `xml:"GivenName"`
	Surname        string       `xml:"Surname"`
	CompanyName    string       `xml:"CompanyName"`
	Department     string       `xml:"Department"`
	OfficeLocation string       `xml:"OfficeLocation"`
	JobTitle       string       `xml:"JobTitle"`
	PhoneNumbers   phoneNumbers `xml:"PhoneNumbers"`
}

type phoneNumbers struct {
	Entries []phoneEntry `xml:"Entry"`
}

type phoneEntry struct {
	Key   string `xml:"Key,attr"`
	Value string `xml:",chardata"`
}

func parseResolveNamesMessage(respBody []byte) (resolveNamesResponseMessage, bool, error) {
	var env envelope
	if err := xml.Unmarshal(respBody, &env); err != nil {
		return resolveNamesResponseMessage{}, false, err
	}

	msg := env.Body.ResolveNamesResponse.ResponseMessages.ResolveNamesResponseMessage
	if msg.ResponseClass == "" && msg.ResponseCode == "" {
		return resolveNamesResponseMessage{}, false, nil
	}

	return msg, true, nil
}

// ResolveNames calls the EWS ResolveNames operation for the given prefix.
func ResolveNames(client *http.Client, url, user, pass, prefix string) ([]Contact, bool, error) {
	body := fmt.Sprintf(soapEnvelopeTemplate, xmlEscape(prefix))

	req, err := http.NewRequest("POST", url, bytes.NewBufferString(body))
	if err != nil {
		return nil, false, &ResponseError{
			Op:     "ResolveNames",
			Kind:   ErrorKindTransport,
			Detail: "creating request",
			Cause:  err,
		}
	}

	req.SetBasicAuth(user, pass)
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")

	resp, err := client.Do(req)
	if err != nil {
		return nil, false, &ResponseError{
			Op:     "ResolveNames",
			Kind:   ErrorKindTransport,
			Detail: "request failed",
			Cause:  err,
		}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, &ResponseError{
			Op:     "ResolveNames",
			Kind:   ErrorKindTransport,
			Detail: "reading response",
			Cause:  err,
		}
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, false, &ResponseError{
			Op:         "ResolveNames",
			Kind:       ErrorKindAuth,
			StatusCode: http.StatusUnauthorized,
			Detail:     "authentication failed",
		}
	}

	msg, recognized, parseErr := parseResolveNamesMessage(respBody)
	if recognized {
		if msg.ResponseCode == "ErrorNameResolutionNoResults" {
			return nil, false, nil
		}
		if msg.ResponseClass == "Error" {
			return nil, false, &ResponseError{
				Op:   "ResolveNames",
				Kind: classifyResponseCode(msg.ResponseCode),
				Code: msg.ResponseCode,
			}
		}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, false, &ResponseError{
			Op:         "ResolveNames",
			Kind:       ErrorKindServer,
			StatusCode: resp.StatusCode,
			Detail:     truncate(string(respBody), 200),
		}
	}

	if parseErr != nil {
		return nil, false, &ResponseError{
			Op:     "ResolveNames",
			Kind:   ErrorKindParse,
			Detail: "parsing XML",
			Cause:  parseErr,
		}
	}
	if !recognized {
		return nil, false, &ResponseError{
			Op:     "ResolveNames",
			Kind:   ErrorKindParse,
			Detail: "unexpected EWS response",
		}
	}

	var contacts []Contact
	for _, r := range msg.ResolutionSet.Resolutions {
		c := Contact{
			DisplayName:  r.Mailbox.Name,
			EmailAddress: r.Mailbox.EmailAddress,
			Title:        r.Contact.JobTitle,
			Department:   r.Contact.Department,
			Office:       r.Contact.OfficeLocation,
			Company:      r.Contact.CompanyName,
			GivenName:    r.Contact.GivenName,
			Surname:      r.Contact.Surname,
		}
		// Grab first phone number available
		for _, p := range r.Contact.PhoneNumbers.Entries {
			if p.Value != "" {
				c.Phone = p.Value
				break
			}
		}
		contacts = append(contacts, c)
	}

	truncated := resolutionSetTruncated(msg.ResolutionSet)

	return contacts, truncated, nil
}

func resolutionSetTruncated(set resolutionSet) bool {
	if set.IncludesLastItemInRange != "" {
		return !strings.EqualFold(set.IncludesLastItemInRange, "true")
	}

	// Fall back to the historical heuristic if the attribute is absent.
	return set.TotalItemsInView >= 100
}

func xmlEscape(s string) string {
	var buf bytes.Buffer
	if err := xml.EscapeText(&buf, []byte(s)); err != nil {
		return s
	}
	return buf.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
