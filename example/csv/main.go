package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/parquet-go/parquet-go"

	"github.com/carlohamalainen/antarctic-database-go"
)

type RowTypeV1 struct {
	MeetingName string
	Number      int
	Revision    int
	Name        string
	MeetingCity string
	MeetingID   int
	MeetingYear int
	Agendas     []string
	Parties     []string
}

func main() {
	meetingType := ats.MeetingType_ATCM_Antarctic_Treaty_Consultative_Meeting
	meeting := ats.Meeting_Integer_ATCM_XII_Canberra_1983
	party := ats.Party_All
	paperType := ats.PaperType_WP
	category := ats.Category_All

	page := 1

	header := []string{"Meeting_Name", "Number", "Revision", "Name", "Meeting_City", "Meeting_ID", "Meeting_Year", "Agendas", "Parties"}

	rows := [][]string{}
	rows = append(rows, header)

	prows := []RowTypeV1{}

	for page > 0 {
		url := ats.BuildSearchMeetingDocuments(meetingType, meeting, party, paperType, category, page)

		resp, err := http.Get(url)
		if err != nil {
			panic(err)
		}
		if resp == nil {
			panic("nil resp")
		}
		defer resp.Body.Close()

		document := ats.Document{}
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

			prow := RowTypeV1{
				MeetingName: item.Meeting_name,
				Number:      item.Number,
				Revision:    item.Revision,
				Name:        item.Name,
				MeetingCity: item.Meeting_city,
				MeetingID:   item.Meeting_id,
				MeetingYear: item.Meeting_year,
				Agendas:     agendaNumbers,
				Parties:     partyNames,
			}

			prows = append(prows, prow)
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

	pfile, perr := os.Create("meetings.parquet")
	if perr != nil {
		panic(perr)
	}
	defer pfile.Close()

	pwriter := parquet.NewGenericWriter[RowTypeV1](pfile)
	defer pwriter.Close()

	_, werr := pwriter.Write(prows)
	if werr != nil {
		panic(werr)
	}
}
