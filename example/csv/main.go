package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/carlohamalainen/antarctic-database-go"
)

func main() {
	meetingType := api.MeetingType_ATCM_Antarctic_Treaty_Consultative_Meeting
	meeting := api.Meeting_Integer_ATCM_XII_Canberra_1983
	party := api.Party_All
	paperType := api.PaperType_WP
	category := api.Category_All

	page := 1

	header := []string{"Meeting_Name", "Number", "Revision", "Name", "Meeting_City", "Meeting_ID", "Meeting_Year", "Agendas", "Parties"}

	rows := [][]string{}
	rows = append(rows, header)

	for page > 0 {
		url := api.BuildSearchMeetingDocuments(meetingType, meeting, party, paperType, category, page)

		resp, err := http.Get(url)
		if err != nil {
			panic(err)
		}
		defer resp.Body.Close()

		document := api.Document{}
		if err := json.NewDecoder(resp.Body).Decode(&document); err != nil {
			panic(err)
		}

		for _, item := range document.Payload {
			agendaNumbers := []string{}
			for _, agenda := range item.Agendas {
				agendaNumbers = append(agendaNumbers, agenda.Number)
			}
			partyNames := []string{}
			for _, partay := range item.Parties {
				partyNames = append(partyNames, partay.Name)
			}

			row := []string{}

			row = append(row, item.Meeting_name)
			row = append(row, fmt.Sprintf("%d", item.Number))
			row = append(row, fmt.Sprintf("%d", item.Revision))
			row = append(row, item.Name)
			row = append(row, item.Meeting_city)
			row = append(row, fmt.Sprintf("%d", item.Meeting_id))
			row = append(row, fmt.Sprintf("%d", item.Meeting_year))
			row = append(row, strings.Join(agendaNumbers, " | "))
			row = append(row, strings.Join(partyNames, " | "))

			rows = append(rows, row)
		}

		page = int(document.Pager.Next)
	}

	writer := csv.NewWriter(os.Stdout)

	err := writer.WriteAll(rows)
	if err != nil {
		panic(err)
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		panic(err)
	}
}
