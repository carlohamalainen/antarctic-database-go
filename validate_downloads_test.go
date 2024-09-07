package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"

	"testing"
)

func TestFirst(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	meetingType := MeetingType_ATCM_Antarctic_Treaty_Consultative_Meeting
	meeting := Meeting_Integer_ATCM_46_CEP_26_Kochi_2024
	party := Party_All
	paperType := PaperType_IP
	category := Category_Safety_and_Operations_in_Antarctica

	page := 1

	expectedNrPages := 3
	nrPages := 0

	expectedNrLinks := 38
	nrLinks := 0

	for page > 0 {
		url := BuildSearchMeetingDocuments(meetingType, meeting, party, paperType, category, page)

		slog.Info("get", "url", url)

		resp, err := http.Get(url)
		if err != nil {
			t.Fatalf("get failed: %v", err)
		}
		defer resp.Body.Close()

		document := Document{}
		if err := json.NewDecoder(resp.Body).Decode(&document); err != nil {
			t.Fatalf("could not decode json: %v", err)
		}

		for _, item := range document.Payload {
			downloadURLs := DownloadLinks(item)

			for _, downloadURL := range downloadURLs {
				logger.Info("validating document link", "url", downloadURL.Url)

				valid, err := ValidateDocumentLink(downloadURL.Url)
				if err != nil {
					t.Fatalf("failed to validate %s due to: %s", downloadURL, err)
				}
				if !valid {
					t.Fatalf("invalid download link: %s", downloadURL)
				}

				nrLinks += 1

				for _, attachment := range item.Attachments {
					attachmentURL := AttachmentLink(attachment)

					logger.Info("validating attachment link", "url", attachmentURL.Url)
					valid, err := ValidateDocumentLink(attachmentURL.Url)
					if err != nil {
						t.Fatalf("failed to validate %s due to: %s", attachmentURL, err)
					}
					if !valid {
						t.Fatalf("invalid download link: %s", attachmentURL)
					}
					nrLinks += 1
				}
			}
		}

		nrPages += 1
		page = document.Pager.Next
	}

	if nrPages != expectedNrPages {
		t.Errorf("expected %d pages but processed %d", expectedNrPages, nrPages)
	}

	if nrLinks != expectedNrLinks {
		t.Errorf("expected %d links but processed %d", expectedNrLinks, nrLinks)
	}
}
