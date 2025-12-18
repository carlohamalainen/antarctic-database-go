package main

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/carlohamalainen/antarctic-database-go"
	"github.com/carlohamalainen/antarctic-database-go/cache"
)

func main() {
	client, err := cache.NewHTTPClient(nil)
	if err != nil {
		panic(err)
	}

	meetingType := ats.MeetingType_ATCM_Antarctic_Treaty_Consultative_Meeting
	meeting := ats.Meeting_Integer_ATCM_46_CEP_26_Kochi_2024
	party := ats.Party_COMNAP
	paperType := ats.PaperType_IP
	category := ats.Category_Safety_and_Operations_in_Antarctica

	page := 1

	for page > 0 {
		url := ats.BuildSearchMeetingDocuments(meetingType, meeting, party, paperType, category, page)

		fmt.Println(url)

		resp, err := http.Get(url)
		if err != nil {
			panic(err)
		}
		if resp == nil {
			panic("nil response when fetching url")
		}
		defer resp.Body.Close()

		document := ats.Document{}
		if err := json.NewDecoder(resp.Body).Decode(&document); err != nil {
			panic(err)
		}

		fmt.Printf("%+v\n", document)

		fmt.Println(len(document.Payload))

		for _, item := range document.Payload {
			downloadURLs := ats.DownloadLinks(item)

			for _, downloadURL := range downloadURLs {
				valid, err := ats.ValidateDocumentLink(client, downloadURL.Url)
				if err != nil {
					panic(err)
				}
				if !valid {
					panic("invalid: " + downloadURL.Url)
				}
				fmt.Printf("%s\n%s\n\n", item.Name, downloadURL)
			}

			fmt.Printf("PARTIES: %+v\n", item.Parties)

			for _, attachment := range item.Attachments {
				attachmentURL := ats.AttachmentLink(attachment)

				valid, err := ats.ValidateDocumentLink(client, attachmentURL.Url)
				if err != nil {
					panic(err)
				}
				if !valid {
					panic("invalid: " + attachmentURL.Url)
				}
				fmt.Println(attachmentURL.Url)
			}
		}

		page = document.Pager.Next
	}
}
