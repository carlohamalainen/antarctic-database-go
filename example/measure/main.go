package main

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/carlohamalainen/antarctic-database-go"
)

func main() {
	// https://www.ats.aq/devAS/ToolsAndResources/SearchDatabase?from=05/30/2024&to=05/30/2024&cat=1&top=163&type=2&stat=3&txt=&curr=0&page=1
	//
	// then
	//
	// https://www.ats.aq/devAS/Meetings/Measure/813?s=1&iframe=1&from=05/30/2024&to=05/30/2024&cat=1&top=163&type=2&stat=3&txt=&curr=0

	meeting := ats.Meeting_Date_ATCM_46_CEP_26_Kochi_2024
	cat := ats.Cat_Area_protection_and_management
	topic := ats.Topic_ASPA_116_New_College_Valley
	docType := ats.DocType_Measure
	status := ats.Status_Not_yet_effective

	url := ats.BuildTreatySearchUrl(
		meeting,
		cat,
		topic,
		docType,
		status,
		1,
	)

	fmt.Println(url)

	resp, err := http.Get(url)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	document := ats.Treaty{}
	if err := json.NewDecoder(resp.Body).Decode(&document); err != nil {
		panic(err)
	}

	fmt.Printf("%+v\n", document)

	url2 := ats.BuildMeasureSearchUrl(
		meeting,
		cat,
		topic,
		docType,
		status,
		int(document.Payload[0].Arecid))

	fmt.Println(url2)

	resp, err = http.Get(url2)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		panic(fmt.Sprintf("bad response code %d on %s", resp.StatusCode, url2))
	}

	measure := ats.ParseMeasure(url2, resp.Body)

	fmt.Printf("%+v\n", measure)
}
