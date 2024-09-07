package main

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/carlohamalainen/antarctic-database-go"
)

func main() {
	meetingType := api.MeetingType_ATCM_Antarctic_Treaty_Consultative_Meeting
	meeting := api.Meeting_Integer_ATCM_46_CEP_26_Kochi_2024
	party := api.Party_COMNAP
	paperType := api.PaperType_IP
	category := api.Category_Safety_and_Operations_in_Antarctica

	page := 1

	for page > 0 {
		url := api.BuildSearchMeetingDocuments(meetingType, meeting, party, paperType, category, page)

		fmt.Println(url)

		resp, err := http.Get(url)
		if err != nil {
			panic(err)
		}
		defer resp.Body.Close()

		document := api.Document{}
		if err := json.NewDecoder(resp.Body).Decode(&document); err != nil {
			panic(err)
		}

		fmt.Printf("%+v\n", document)

		fmt.Println(len(document.Payload))

		for _, item := range document.Payload {
			downloadURLs := api.DownloadLinks(item)

			for _, downloadURL := range downloadURLs {
				valid, err := api.ValidateDocumentLink(downloadURL.Url)
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
				attachmentURL := api.AttachmentLink(attachment)

				valid, err := api.ValidateDocumentLink(attachmentURL.Url)
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
