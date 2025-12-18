package main

import (
	"bytes"
	"crypto/sha256"
	"flag"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	// "runtime/pprof"

	"github.com/yosssi/gohtml"

	"github.com/PuerkitoBio/goquery"

	"github.com/dop251/goja"
	"github.com/dop251/goja/parser"

	"github.com/carlohamalainen/antarctic-database-go"
	"github.com/carlohamalainen/antarctic-database-go/cache"
)

const informationExchangeURL = "https://www.ats.aq/devAS/InformationExchange/ArchivedInformation?lang=e"

// ExclusionList defines sections to be excluded from processing
var ExcludedSections = []string{
	"Scientific Information - Forward Plans",
	"Scientific Information - Science Activities in Previous Year",
	"Environmental Information - Conservation of Fauna and Flora",
	"Environmental Information - Area Protection and Management",
}

var timeout time.Duration

var client *http.Client

var chunkdIDs map[string]bool

func getURLs(report Report, language Language) []ats.URL {
	switch language {
	case LanguageEnglish:
		return report.English
	case LanguageSpanish:
		return report.Spanish
	case LanguageFrench:
		return report.French
	case LanguageRussian:
		return report.Russian
	default:
		panic("unknown language: " + language)
	}
}

func ExpandReports(reportCollection ReportCollection) ([]Expanded, error) {
	expanded := make([]Expanded, 0)

	periods := []Period{ReportPeriodPreSeason, ReportPeriodAnnual}

	for _, period := range periods {
		for _, report := range reportCollection.getReports(period) {
			for _, language := range Languages() {
				for _, url := range getURLs(report, language) {
					if !strings.Contains(string(url), "https://eies.ats.aq/Report") {
						slog.Info("skipping", "url", url, "language", language)
						continue
					}

					// debugging
					// Intercontental flights from Hobart to Wilkins Aerodrome to transport passengers and cargo
					// if url != "https://eies.ats.aq/Report/GenRpt?idParty=2&period=1&idYear=2018&lang=e" {
					// 	continue
					// }

					document, _, _, err := ats.Download(url, timeout, client) // FIXME check 200
					if err != nil {
						return nil, err
					}

					goqueryDocument, err := goquery.NewDocumentFromReader(io.NopCloser(bytes.NewReader(document)))
					if err != nil {
						return nil, err
					}

					expanded = append(expanded, Expanded{
						Period:          period,
						Country:         report.Country,
						Language:        language,
						URL:             url,
						Document:        string(document),
						GoqueryDocument: goqueryDocument,
					})
				}
			}
		}
	}

	return expanded, nil
}

func GetRptEndpoints(expanded []Expanded) error {
	for i, exp := range expanded {
		var getRpt *string

		for _, s := range exp.GoqueryDocument.Find("script").EachIter() {
			scriptText := s.Text()
			if strings.Contains(scriptText, "getRptEndpoint") {
				program, err := parser.ParseFile(nil, "script.js", scriptText, 0)
				if err != nil {
					e := fmt.Errorf("parse error: %w", err)
					slog.Error(e.Error())
					return e
				}

				gr := ats.MatchFunction(program, "getRptEndpoint")

				if gr != nil {
					getRpt = gr
					break
				}
			}

		}

		if getRpt == nil {
			e := fmt.Errorf("did not find getRpt")
			slog.Error(e.Error())
			return e
		}

		expanded[i].GetRptEndpointFn = *getRpt
	}

	return nil
}

func ExtractSectionInfo(s *goquery.Selection) *Section {
	section := Section{}

	// Extract the report ID from onclick attribute
	button := s.Find("button.js-accordion-button")
	onclick, _ := button.Attr("onclick")
	if onclick != "" {
		// Extract content between single quotes
		if matches := regexp.MustCompile(`'([^']+)'`).FindStringSubmatch(onclick); len(matches) > 1 {
			section.ReportID = matches[1]
		}
	}

	// Extract the section title
	section.Title = s.Find(".accordion__item__button__text").Text()

	if slices.Contains(ExcludedSections, section.Title) {
		return nil
	}

	slog.Info("extracting section", "title", section.Title)

	// Determine section type
	if s.Find("li.accordion__disabled").Length() > 0 {
		section.Type = SectionNotYetPublished
	} else if s.Find("button.accordion__item__button_ntr").Length() > 0 {
		section.Type = SectionNothingToReport
	} else {
		section.Type = SectionPublished
	}

	return &section
}

func GetSections(expanded []Expanded) error {
	for idx, exp := range expanded {
		for _, s := range exp.GoqueryDocument.Find(".js-accordion.accordion").EachIter() {
			section := ExtractSectionInfo(s)
			if section == nil {
				// these are excluded sections, not errors
				continue
			}
			expanded[idx].Sections = append(expanded[idx].Sections, *section)
		}
	}

	return nil
}

func runGetRptEndpoint(js string, input string) (goja.Value, error) {
	vm := goja.New()

	_, err := vm.RunString(js)
	if err != nil {
		slog.Error("failed to run js string", "js", js, "err", err)
		return nil, err
	}

	getRptEndpoint := vm.Get("getRptEndpoint")
	fn, ok := goja.AssertFunction(getRptEndpoint)
	if !ok {
		err = fmt.Errorf("found getRptEndpoint but it is not a function")
		slog.Error(err.Error())
		return nil, err
	}

	result, err := fn(goja.Undefined(), vm.ToValue(input))
	if err != nil {
		err = fmt.Errorf("failed to convert result: %w", err)
		slog.Error(err.Error())
		return nil, err
	}

	return result, nil
}

func DownloadSections(expanded []Expanded) error {
	const base = "https://eies.ats.aq/Report"

	for i0, exp := range expanded {
		for i1, section := range exp.Sections {
			if section.Type == SectionPublished {
				endpoint, err := runGetRptEndpoint(exp.GetRptEndpointFn, section.ReportID)
				if err != nil {
					slog.Error(err.Error())
					return err
				}

				url := fmt.Sprintf("%s/%s", base, endpoint)

				body, _, _, err := ats.Download(ats.URL(url), timeout, client) // FIXME check 200
				if err != nil {
					slog.Error(err.Error())
					return err
				}

				body = fixup(url, body)

				slog.Debug("getting items for section", "country", exp.Country, "len", len(body), "url", url)

				expanded[i0].Sections[i1].RawBody = string(body)

				// debugging

				// if url == "https://eies.ats.aq/Report/OpShipBasedRpt?period=2&year=2023&idParty=42" {
				// 	os.WriteFile("body.html", []byte(body), 0644)
				// }

				// if strings.Contains(body, "Antarpply") && strings.Contains(body, "Wild, cabo") && strings.Contains(body, "03 Nov 2024") {
				// 	os.WriteFile("body_antarpply.html", []byte(body), 0644)
				// }
			}
		}
	}

	return nil
}

func expandHeaders(s *goquery.Selection) ([]string, error) {
	headers := []string{}
	knownHeaders := map[string]int{}

	for j, t := range s.Find("table.table__data thead th").EachIter() {
		headerText := t.Find("center").Text()
		if headerText == "" {
			headerText = t.Text()
		}
		headerText = normalizeHeader(headerText)

		k, ok := knownHeaders[headerText]

		switch {
		case ok && headers[k] != headerText:
			return nil, fmt.Errorf("mismatch in headers, expected %s but found %s at offset %d", headers[k], headerText, k)
		case ok && headers[k] == headerText:
			// this is fine, a match
		case !ok:
			// new header value, this is ok
			knownHeaders[headerText] = j
			headers = append(headers, headerText)
		}
	}

	return headers, nil
}

func expandVisits(headers []string, s *goquery.Selection) ([]Visit, error) {
	var currentHeader *goquery.Selection
	var currentValues []*goquery.Selection

	visits := make([]Visit, 0)

	var visit *Visit

	for _, tr := range s.Find("tbody.table__results tr").EachIter() {
		// Check if this is a plain tr (header)
		style, exists := tr.Attr("style")
		if !exists {
			// If we have a previous header and values, print them
			if currentHeader != nil {
				if visit == nil {
					panic(fmt.Errorf("trying to update nil valued visit"))
				}
				for z, cell := range currentHeader.Find("td").EachIter() {
					if z < len(headers) {
						visit.Header[headers[z]] = strings.TrimSpace(cell.Text())
					} else {
						panic(fmt.Errorf("out of bounds index %d with %d known headers", z, len(headers)))
					}
				}

				for _, cv := range currentValues {
					visit.Visits = append(visit.Visits, extractSiteInfo(cv))
				}

				visits = append(visits, *visit)
			}

			// Start a new group
			currentHeader = tr
			currentValues = []*goquery.Selection{}
			visit = new(Visit)
			visit.Header = make(map[string]string)
			visit.Visits = make([]map[string]string, 0)

		} else if strings.Contains(style, "important") {
			// This is a value tr, add it to current group
			currentValues = append(currentValues, tr)
		}
	}

	// Print the last group
	if currentHeader != nil {
		visit = new(Visit)
		visit.Header = make(map[string]string)
		visit.Visits = make([]map[string]string, 0)

		for z, cell := range currentHeader.Find("td").EachIter() {
			if z < len(headers) {
				visit.Header[headers[z]] = strings.TrimSpace(cell.Text())
			} else {
				panic(fmt.Errorf("out of bounds index %d with %d known headers", z, len(headers)))
			}
		}

		for _, cv := range currentValues {
			visit.Visits = append(visit.Visits, extractSiteInfo(cv))
		}

		visits = append(visits, *visit)
	}

	return visits, nil
}

func ExpandSections(expanded []Expanded) error {
	var err error

	for i0, exp := range expanded {
		for i1, section := range exp.Sections {
			section.Chunks, err = ExpandChunks(exp.URL, section.RawBody)

			if err != nil {
				return err
			}

			expanded[i0].Sections[i1] = section
		}
	}

	return nil
}

func extractFlattenedHeaders(sel *goquery.Selection, data map[string]ValueData) {
	prefix := ""

	// debugging

	// if strings.Contains(sel.Text(), "Operator") {
	// 	PrettyPrintSelection(sel)
	// 	fmt.Println("operator")
	// }

	for _, selHeader := range sel.Find("div.report--item__header").EachIter() {
		keys := []string{}
		values := []ValueData{}

		for _, selTitleKey := range selHeader.Find("div.report--item--header--title__key").EachIter() {
			keyText := strings.TrimSpace(selTitleKey.Clone().Children().Remove().End().Text())
			keys = append(keys, keyText)
		}

		for i, selValue := range selHeader.Find("div.report--item--header--title__value").EachIter() {
			value := strings.TrimSpace(selValue.Text())
			value = html.UnescapeString(value)
			value = strings.ReplaceAll(value, "\u00A0", "") // Remove non-breaking space

			rawHTML, _ := selValue.Html()
			values = append(values, ValueData{RawHTML: rawHTML, Text: value})

			if i >= len(keys) {
				panic(fmt.Errorf("out of bounds %d with %d known keys", i, len(keys)))
			}
		}

		for i, k := range keys {
			// FIXME special case sigh; e.g. https://eies.ats.aq/Report/GenRpt?idParty=1&period=1&idYear=2024&lang=e in the section "... non gov - vessel based operations"
			// OPERATOR
			//         NAME: ANTARPPLY EXPEDITIONS
			//         CONTACT ADDRESS: AV. RIVADAVIA 2206 - 8°B - C1034ACO- BUENOS AIRES
			//         EMAIL ADDRESS: INFORMATION@ANTARPPLY.COM
			//         WEBSITE ADDRESS: WWW.ANTARPPLY.COM

			if k == "Operator" {
				// These values are picked up below as they are `li` pieces.
				prefix = "Operator."
				continue
			}
			data[k] = values[i]
		}
	}

	// FIXME this should select into the body

	for _, s := range sel.Find("ul.report--ul__key li").EachIter() {
		// Get the text content of the li element without the span

		keyText := strings.TrimSpace(s.Clone().Children().Remove().End().Text())
		keyText = strings.TrimSuffix(keyText, ":")
		keyText = prefix + keyText

		valueSpan := s.Find("span.report--item--header--title__value")
		value := strings.TrimSpace(valueSpan.Text())

		value = html.UnescapeString(value)
		value = strings.ReplaceAll(value, "\u00A0", "") // Remove non-breaking space

		rawHTML, _ := s.Html()
		data[keyText] = ValueData{RawHTML: rawHTML, Text: value}
	}

	// Process the rows in each report--item__body
	for _, s := range sel.Find(".report--item__body .row").EachIter() {
		// Find all keys in this row
		keys := make([]string, 0)
		for _, k := range s.Find(".report--item--header--title__key").EachIter() {
			key := strings.TrimSpace(k.Text())
			if key != "" {
				keys = append(keys, key)
			} else {
				panic("empty key text")
			}
		}

		// Find all values in this row
		values := make([]string, 0)
		for _, v := range s.Find(".report--item--header--title__value").EachIter() {
			value := strings.TrimSpace(v.Text())
			value = strings.ReplaceAll(value, "\n", "")
			value = strings.Join(strings.Fields(value), " ")
			value = html.UnescapeString(value)
			values = append(values, value)
		}

		// Match keys with values
		for j, key := range keys {
			// special case - these were captured in the "li" items above.
			if key == "Operator" {
				continue
			}
			if j < len(values) {
				rawHTML, _ := s.Html()
				data[key] = ValueData{RawHTML: rawHTML, Text: values[j]}
			} else {
				slog.Info("no key values for this section") // FIXME log, report on these, etc, e.g. https://eies.ats.aq/Report/GenRpt?idParty=1&period=2&idYear=2023&lang=e "science activities in previous year"
			}
		}
	}
}

func createChunkId(url string, i int, data map[string]ValueData, text string) string {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder

	sb.WriteString(url)

	sb.WriteString(fmt.Sprintf("chunk offset %d", i))

	for _, k := range keys {
		sb.WriteString(k)
		sb.WriteString(":")
		sb.WriteString(data[k].Text)
		sb.WriteString(";")
	}

	sb.WriteString(text)

	h := sha256.New()
	h.Write([]byte(sb.String()))

	return fmt.Sprintf("%x", h.Sum(nil)[:7])
}

type SelectionSlice []*goquery.Selection

func (s SelectionSlice) Len() int      { return len(s) }
func (s SelectionSlice) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

func (s SelectionSlice) Less(i, j int) bool {
	return s[i].Text() < s[j].Text()
}

func ExpandChunks(url ats.URL, body string) ([]Chunk, error) {
	if chunkdIDs == nil {
		return nil, fmt.Errorf("global chunkIDs is nil")
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		return nil, err
	}

	chunks := make([]Chunk, 0)

	// debugging

	if strings.Contains(body, "Davis Station Resupply") {
		fmt.Println("https://eies.ats.aq/Report/GenRpt?idParty=2&period=2&idYear=2023&lang=e")
	}

	if strings.Contains(body, "Electrochemical Concentration Cell") {
		fmt.Println("https://eies.ats.aq/Report/OpResearchRocketsRpt?period=1&year=2015&idParty=23")
	}

	// if strings.Contains(body, "Antarpply") && strings.Contains(body, "Wild, cabo") && strings.Contains(body, "03 Nov 2024") {
	// 	fmt.Println("Wild cabo")
	// }

	// if strings.Contains(body, "Air link between Australia and Antarctica for AAP passengers") {
	// 	fmt.Println("AAP")
	// }

	// if strings.Contains(body, "Fixed Wing Airbus A319") {
	// 	fmt.Println("A319")
	// }

	selections := make(SelectionSlice, 0)

	for _, s := range doc.Find(".report--item").EachIter() {
		selections = append(selections, s)
	}

	sort.Sort(selections)

	for i, s := range selections {
		chunk := Chunk{}

		chunk.MainHeader = make(map[string]ValueData)

		extractFlattenedHeaders(s, chunk.MainHeader)

		chunk.Visited, err = extractVisited(s)
		if err != nil {
			return nil, err
		}

		chunk.Id = createChunkId(string(url), i, chunk.MainHeader, s.Text())

		_, exists := chunkdIDs[chunk.Id]
		if exists {
			fmt.Println(s.Text())
			panic(fmt.Errorf("hash collision for chunk ID: %s", chunk.Id))
		}

		chunkdIDs[chunk.Id] = true

		chunks = append(chunks, chunk)
	}

	return chunks, nil
}

func extractVisited(sel *goquery.Selection) ([]Visit, error) {
	// fmt.Println("extractVisited sel:")
	// fmt.Println(sel.Html()) // FIXME

	visits := make([]Visit, 0)

	for _, s := range sel.Find("div.report--item--body__table").EachIter() {
		headers, err := expandHeaders(s)
		if err != nil {
			return nil, err
		}

		// fmt.Printf("extractVisited headers: %+v\n", headers)
		// _html, _ := s.Html()
		// fmt.Printf("extractVisited focused on s:\n%s\n", _html) // FIXME

		vs, err := expandVisits(headers, s)
		if err != nil {
			return nil, err
		}

		visits = append(visits, vs...)
	}

	return visits, nil
}

func GetLocations(expanded []Expanded) error {
	for i0, exp := range expanded {
		for i1, section := range exp.Sections {
			for i2, chunk := range section.Chunks {
				for k, v := range chunk.MainHeader {
					if k == "Location" || k == "Locations:" {
						location, err := parseLocation(v.RawHTML)
						if err != nil {
							slog.Error(err.Error())
							os.Exit(1)
						}

						newValue := expanded[i0].Sections[i1].Chunks[i2].MainHeader[k]
						newValue.Location = append(newValue.Location, location)

						expanded[i0].Sections[i1].Chunks[i2].MainHeader[k] = newValue

						slog.Debug("parsed location", "key", k, "location", fmt.Sprintf("%+v", location))
					}
				}
			}
		}
	}

	return nil
}

func getLatLon(loc LocationP) (float64, float64, error) {
	latDMD := loc.LatitudeDMD.ToDecimalDegrees()
	lonDMD := loc.LongitudeDMD.ToDecimalDegrees()

	latDMS := loc.LatitudeDMS.ToDecimalDegrees()
	lonDMS := loc.LongitudeDMS.ToDecimalDegrees()

	if latDMD != 0 && lonDMD != 0 {
		return latDMD, lonDMD, nil
	}

	if latDMS != 0 && lonDMS != 0 {
		return latDMS, lonDMS, nil
	}

	if loc.LatitudeRaw == "" && loc.LongitudeRaw == "" {
		return 0, 0, nil
	}

	// Geographical South Pole
	if latDMD == -90 && lonDMD == 0 {
		return latDMD, lonDMD, nil
	}
	if latDMS == -90 && lonDMS == 0 {
		return latDMS, lonDMS, nil
	}

	// Mix of formats!
	//
	// "</span>Paradise Cove"
	// "<span><strong>Lat</strong>: </span>62°13´S\u00a0\u00a0-"
	// "<span><strong>Long</strong>: </span>58°26´30´´W"
	if latDMD != 0 && lonDMS != 0 {
		return latDMD, lonDMS, nil
	}
	if latDMS != 0 && lonDMD != 0 {
		return latDMS, lonDMD, nil
	}

	if loc.LatitudeRaw == emptyLatitude && loc.LongitudeRaw == emptyLongitude {
		return 0, 0, nil
	}

	return 0, 0, fmt.Errorf("loc weird: %+v", loc)
}

func flattenExpeditionRecords(expanded []Expanded) ([]ExpeditionRecord, error) {

	expeditionRecords := []ExpeditionRecord{}

	for _, exp := range expanded {
		for _, section := range exp.Sections {
			for _, chunk := range section.Chunks {
				for _, visit := range chunk.Visited {

					expeditionRecord := ExpeditionRecord{}

					expeditionRecord.URL = string(exp.URL)
					expeditionRecord.Period = string(exp.Period)
					expeditionRecord.Country = exp.Country
					expeditionRecord.Language = string(exp.Language)
					expeditionRecord.SectionTitle = section.Title
					expeditionRecord.ReportID = section.ReportID

					cleanMainHeader := map[string]string{}
					for k, v := range chunk.MainHeader {
						cleanMainHeader["expedition_"+normalizeHeader2(k)] = v.Text
					}

					cleanVisitHeader := map[string]string{}
					for k, v := range visit.Header {
						cleanVisitHeader["visit_"+normalizeHeader2(k)] = v
					}

					err := PopulateFromMapByTag(&expeditionRecord, cleanMainHeader)
					if err != nil {
						return nil, fmt.Errorf("failed to set all header values from cleanMainHeader: %w", err)
					}

					err = PopulateFromMapByTag(&expeditionRecord, cleanVisitHeader)
					if err != nil {
						return nil, fmt.Errorf("failed to set all header values from cleanVisitHeader: %w", err)
					}

					expeditionRecords = append(expeditionRecords, expeditionRecord)
				}

				if len(chunk.Visited) == 0 {
					expeditionRecord := ExpeditionRecord{}

					expeditionRecord.URL = string(exp.URL)
					expeditionRecord.Period = string(exp.Period)
					expeditionRecord.Country = exp.Country
					expeditionRecord.Language = string(exp.Language)
					expeditionRecord.SectionTitle = section.Title
					expeditionRecord.ReportID = section.ReportID

					cleanMainHeader := map[string]string{}
					for k, v := range chunk.MainHeader {
						cleanMainHeader["expedition_"+normalizeHeader2(k)] = v.Text
					}

					err := PopulateFromMapByTag(&expeditionRecord, cleanMainHeader)
					if err != nil {
						return nil, fmt.Errorf("failed to set all header values from cleanVisitHeader in empty Visited case: %w", err)
					}

					expeditionRecords = append(expeditionRecords, expeditionRecord)

				}

			}
		}
	}

	return expeditionRecords, nil
}

func flattenFlights(expanded []Expanded) ([]FlightRecord, error) {
	var records []FlightRecord

	for _, exp := range expanded {
		for _, section := range exp.Sections {
			for _, chunk := range section.Chunks {
				for _, visit := range chunk.Visited {

					departureDate, ok := visit.Header["departure_date"]
					if !ok {
						continue
					}

					route := visit.Header["route"]
					purpose := visit.Header["purpose"]
					type_ := chunk.MainHeader["Type"]
					maximumCrew := chunk.MainHeader["Maximum Crew"]
					maximumPassengers := chunk.MainHeader["Maximum Passengers"]
					numberOfFlights := chunk.MainHeader["Number Of Flights"]
					plannedDateFirstFlight := chunk.MainHeader["Planned date of first flight"]
					plannedDateLastFlight := chunk.MainHeader["Planned date of last flight"]
					aircraftAdditionalInfo := chunk.MainHeader["Aircraft additional information"]

					parsedDate, err := time.Parse("02 Jan 2006", departureDate)
					if err != nil {
						slog.Warn("date parsing failed", "url", string(exp.URL), "section", section.Title, "value", departureDate, "error", err)
					}

					rec := FlightRecord{
						URL:                    string(exp.URL),
						Period:                 string(exp.Period),
						Country:                exp.Country,
						Language:               string(exp.Language),
						SectionTitle:           section.Title,
						ReportID:               section.ReportID,
						TableID:                chunk.Id,
						DepartureDate:          parsedDate,
						Route:                  route,
						Purpose:                purpose,
						Type:                   type_.Text,
						MaximumCrew:            maximumCrew.Text,
						MaximumPassengers:      maximumPassengers.Text,
						NumberOfFlights:        numberOfFlights.Text,
						PlannedDateFirstFlight: plannedDateFirstFlight.Text,
						PlannedDateLastFlight:  plannedDateLastFlight.Text,
						AircraftAdditionalInfo: aircraftAdditionalInfo.Text,
					}

					records = append(records, rec)
				}
			}
		}
	}

	return records, nil
}

func flattenVisits(expanded []Expanded) ([]VisitRecord, error) {
	var records []VisitRecord

	for _, exp := range expanded {
		for _, section := range exp.Sections {
			for _, chunk := range section.Chunks {
				for _, visit := range chunk.Visited {
					for _, vv := range visit.Visits {

						lat := vv["lat"]
						lon := vv["long"]

						visitDepartDate := visit.Header["depart_date"]
						visitDepartPort := visit.Header["depart_port"]
						visitArrivalDate := visit.Header["arrival_date"]
						visitArrivalPort := visit.Header["arrival_port"]
						visitExpeditionLeader := visit.Header["expedition_leader"]

						// TODO upstream we should trim this string.
						visitDepartDateP, err := time.Parse("02 Jan 2006", strings.Split(visitDepartDate, "\n")[0])
						if err != nil {
							panic(err)
						}

						// TODO same issue.
						visitArrivalDateP, err := time.Parse("02 Jan 2006", strings.Split(visitArrivalDate, "\n")[0])
						if err != nil {
							panic(err)
						}

						visitDate := vv["visit_date"]
						includesLanding := vv["this_visit_includes_landing"]
						visitSiteName := vv["site_name"]

						latDec := convertLatitudeToDecimal(lat)
						lonDec := convertLongitudeToDecimal(lon)

						vesselName := chunk.MainHeader["Name"]
						vesselCountryOfRegistry := chunk.MainHeader["Country of Registry"]
						vesselMaxCrew := chunk.MainHeader["Maximum Crew"]
						vesselNumberOfVoyages := chunk.MainHeader["Number of Voyages"]

						operatorName := chunk.MainHeader["Operator.Name"]
						operatorWebsiteAddress := chunk.MainHeader["Operator.Website Address"]
						operatorContactAddress := chunk.MainHeader["Operator.Contact Address"]

						parsedDate, err := time.Parse("02/01/2006", visitDate)
						if visitDate != "" && visitDate != "---" && err != nil {
							slog.Warn("date parsing failed", "url", string(exp.URL), "section", section.Title, "value", visitDate, "error", err)
						}

						rec := VisitRecord{
							URL:          string(exp.URL),
							Period:       string(exp.Period),
							Country:      exp.Country,
							Language:     string(exp.Language),
							SectionTitle: section.Title,
							ReportID:     section.ReportID,

							VisitDepartDate:       visitDepartDateP,
							VisitDepartPort:       visitDepartPort,
							VisitArrivalDate:      visitArrivalDateP,
							VisitArrivalPort:      visitArrivalPort,
							VisitExpeditionLeader: visitExpeditionLeader,

							VesselName:              vesselName.Text,
							VesselCountryOfRegistry: vesselCountryOfRegistry.Text,
							VesselMaxCrew:           vesselMaxCrew.Text,
							VesselNumberOfVoyages:   vesselNumberOfVoyages.Text,

							ChunkId: chunk.Id,

							OperatorName:           operatorName.Text,
							OperatorWebsiteAddress: operatorWebsiteAddress.Text,
							OperatorContactAddress: operatorContactAddress.Text,

							VisitSiteName:            visitSiteName,
							VisitLatitude:            latDec,
							VisitLongitude:           lonDec,
							VisitDate:                parsedDate,
							ThisVisitIncludesLanding: includesLanding,
						}

						slog.Debug("flatten_visits", "visit_record", fmt.Sprintf("%+v", rec))

						records = append(records, rec)
					}
				}
			}
		}
	}

	return records, nil
}

func flattenLocations(expanded []Expanded) ([]LocationRecord, error) {
	var records []LocationRecord

	for _, exp := range expanded {
		for _, section := range exp.Sections {
			for _, chunk := range section.Chunks {
				for dataKey, valueData := range chunk.MainHeader {
					// We only create records if there is at least one location
					if len(valueData.Location) == 0 {
						continue
					}

					// Each ValueData may have multiple LocationP entries
					for _, loc := range valueData.Location {

						// no location data, so skip
						if loc.LatitudeRaw == "" && loc.LongitudeRaw == "" {
							continue
						}
						if loc.LatitudeRaw == emptyLatitude && loc.LongitudeRaw == emptyLongitude {
							continue
						}

						lat, lon, err := getLatLon(loc)
						if err != nil {
							slog.Error(err.Error())
							os.Exit(1)
						}

						if lat == 0 && lon == 0 {
							err := fmt.Errorf("zero lat, lon in loc: %+v", loc)
							slog.Error(err.Error())
							os.Exit(1)
						}

						rec := LocationRecord{
							URL:          string(exp.URL),
							Period:       string(exp.Period),
							Country:      exp.Country,
							Language:     string(exp.Language),
							SectionTitle: section.Title,
							ReportID:     section.ReportID,
							Key:          dataKey,
							TextValue:    valueData.Text,

							SiteName:     loc.SiteName,
							LatitudeRaw:  loc.LatitudeRaw,
							LongitudeRaw: loc.LongitudeRaw,

							Latitude:  lat,
							Longitude: lon,
						}

						records = append(records, rec)
					}
				}

			}
		}
	}

	return records, nil
}

func PopulateFromMapByTag(record *ExpeditionRecord, data map[string]string) error {
	used := map[string]struct{}{}

	v := reflect.ValueOf(record).Elem()
	t := v.Type()

	for i := range v.NumField() {
		field := v.Field(i)
		tag := t.Field(i).Tag.Get("parquet")

		// Extract the tag name (before any comma)
		tagName := strings.Split(tag, ",")[0]

		if value, exists := data[tagName]; exists && field.CanSet() {
			used[tagName] = struct{}{}

			field.SetString(value)
		}
	}

	if len(used) < len(data) {
		for k := range data {
			_, ok := used[k]

			if !ok {
				return fmt.Errorf("did not set: %s", k)
			}
		}
	}

	return nil
}

func main() {
	var err error

	// f, err := os.Create("cpu.prof")
	// if err != nil {
	// 	panic(err)
	// }
	// defer f.Close()

	// if err := pprof.StartCPUProfile(f); err != nil {
	// 	panic(err)
	// }
	// defer pprof.StopCPUProfile()
	/////////////////////////////////////////////////////////////////////////////////////

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelDebug,
	}))
	slog.SetDefault(logger)

	timeoutArg := flag.Duration("timeout", 60*time.Second, "Timeout (s)")
	dbFileArg := flag.String("http-cache", "", "Absolute path to sqlite file cache")
	outputDirArg := flag.String("output-dir", "", "Absolute path to output directory")

	flag.Parse()

	timeout = *timeoutArg

	if *dbFileArg == "" {
		slog.Error(fmt.Errorf("need -http-cache").Error())
		os.Exit(1)
	}

	if *outputDirArg == "" {
		slog.Error(fmt.Errorf("need -output-dir").Error())
		os.Exit(1)
	}

	slog.Info("scraper starting")

	client, err = cache.NewHTTPClient(dbFileArg)
	defer func() {
		err := client.Transport.(*cache.Cache).SqliteCache.Close()
		if err != nil {
			logger.Error("failed to close sqlite", "error", err.Error())
		}
	}()

	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}

	html, _, _, err := ats.Download(informationExchangeURL, timeout, client) // FIXME check 200
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}

	reports, err := parseReports(html)
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}

	expanded, err := ExpandReports(reports)
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}

	err = GetRptEndpoints(expanded)
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}

	err = GetSections(expanded)
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}

	// debugging
	// expandedTmp := []Expanded{}
	// for _, exp := range expanded {
	// 	expTmp := exp
	// 	expTmp.Sections = nil

	// 	for _, section := range exp.Sections {
	// 		if section.Title == "Operational Information - National Expeditions - Non-Military Aircraft" {
	// 			// if section.Title == "Operational Information - National Expeditions - Non-Military Ships" {
	// 			expTmp.Sections = append(expTmp.Sections, section)
	// 			slog.Info("keeping")
	// 		} else {
	// 			slog.Info("skipping")
	// 		}
	// 	}

	// 	if len(expTmp.Sections) > 0 {
	// 		expandedTmp = append(expandedTmp, expTmp)
	// 	}
	// }
	// expanded = expandedTmp

	// expanded = make([]Expanded, 1)
	// expanded[0] = expandedTmp[14]

	err = DownloadSections(expanded)
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}

	// ExpandSections and then ExpandChunks uses the global to check for collisions of chunk IDs
	chunkdIDs = make(map[string]bool)

	err = ExpandSections(expanded)
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}

	err = GetLocations(expanded)
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}

	// debugging
	// for _, e := range expanded {
	// 	fmt.Println(e.Period, e.Country, e.Language, e.URL, len(e.Document), len(e.GetRptEndpointFn), len(e.Sections))
	// }

	expeditionRecords, err := flattenExpeditionRecords(expanded)
	if err != nil {
		slog.Error("failed to extract expedition records", "err", err)
		os.Exit(1)
	}

	err = ats.WriteRecords(path.Join(*outputDirArg, "ats-extract-information-exchange-expeditions.parquet"), expeditionRecords)
	if err != nil {
		slog.Error("failed to write parquet", "err", err)
		os.Exit(1)
	}

	locationRecords, err := flattenLocations(expanded)
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}

	outPath := "ats-extract-locations.parquet"
	err = ats.WriteRecords(path.Join(*outputDirArg, outPath), locationRecords)
	if err != nil {
		slog.Error("failed to write parquet", "err", err)
		os.Exit(1)
	}
	slog.Info("wrote parquet successfully", "path", outPath, "count", len(locationRecords))

	visitRecords, err := flattenVisits(expanded)
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}

	outPath = "ats-extract-visits.parquet"
	err = ats.WriteRecords(path.Join(*outputDirArg, outPath), visitRecords)
	if err != nil {
		slog.Error("failed to write parquet", "err", err)
		os.Exit(1)
	}
	slog.Info("wrote parquet successfully", "path", outPath, "count", len(visitRecords))

	flightRecords, err := flattenFlights(expanded)
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}

	outPath = "ats-extract-flights.parquet"
	err = ats.WriteRecords(path.Join(*outputDirArg, outPath), flightRecords)
	if err != nil {
		slog.Error("failed to write parquet", "err", err)
		os.Exit(1)
	}
	slog.Info("wrote parquet successfully", "path", outPath, "count", len(flightRecords))

	slog.Info("scraper finished")
}

func extractSiteInfo(selection *goquery.Selection) map[string]string {
	siteInfo := make(map[string]string)

	selection.Find("span.fw-bolder").Each(func(_ int, label *goquery.Selection) {
		labelText := strings.TrimSpace(strings.TrimSuffix(label.Text(), ":"))
		labelKey := normalizeHeader(labelText)
		valueText := strings.TrimSpace(label.Next().Text())
		siteInfo[labelKey] = valueText
	})

	if len(siteInfo) == 0 {
		// slog.Debug("empty selection")
	}

	return siteInfo
}

func normalizeHeader(header string) string {
	// Remove common HTML artifacts
	header = strings.TrimSpace(header)
	header = strings.ReplaceAll(header, "</center>", "")
	header = strings.ReplaceAll(header, "<center>", "")

	// Convert to lowercase and replace spaces with underscores
	header = strings.ToLower(header)
	header = strings.ReplaceAll(header, " ", "_")
	header = strings.ReplaceAll(header, ".", "")

	return header
}

func normalizeHeader2(header string) string {
	header = strings.ToLower(header)
	header = strings.ReplaceAll(header, " ", "_")
	header = strings.ReplaceAll(header, ".", "_")
	header = strings.ReplaceAll(header, "/", "_")
	header = strings.ReplaceAll(header, "(", "")
	header = strings.ReplaceAll(header, ")", "")

	re := regexp.MustCompile(`_+`)
	header = re.ReplaceAllString(header, "_")

	return header
}

func PrettyPrintSelection(s *goquery.Selection) {
	html, err := goquery.OuterHtml(s)
	if err != nil {
		panic(err)
	}

	// Format the HTML with proper indentation
	prettyHTML := gohtml.Format(html)
	fmt.Println("<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<")
	fmt.Println(prettyHTML)
	fmt.Println(">>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>")
}

func PrettyPrintSelectionsss(s *goquery.Selection) string {
	html, err := goquery.OuterHtml(s)
	if err != nil {
		fmt.Println("Error getting HTML:", err)
		return ""
	}

	// Format the HTML with proper indentation
	return gohtml.Format(html)
}

func fixup(url string, body []byte) []byte {
	slog.Debug("checking fix", "url", url)
	switch url {
	case "https://eies.ats.aq/Report/GenRpt?idParty=7&period=1&idYear=2023&lang=e":
		old := "18/01/1900&nbsp;"
		new := "18/01/2023&nbsp;"
		slog.Info("applying fix", "url", url, "old", old, "new", new)
		return []byte(strings.ReplaceAll(string(body), old, new))
	default:
		return body
	}
}
