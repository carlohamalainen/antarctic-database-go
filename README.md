# antarctic-database-go - a simple Go API to the Antarctic Treaty Database

[![Build Status](https://github.com/carlohamalainen/antarctic-database-go/actions/workflows/test.yml/badge.svg?branch=main)](https://github.com/carlohamalainen/antarctic-database-go/actions)
[![Go Reference](https://pkg.go.dev/badge/github.com/carlohamalainen/antarctic-database-go.svg)](https://pkg.go.dev/github.com/carlohamalainen/antarctic-database-go)
[![Sourcegraph Badge](https://sourcegraph.com/github.com/carlohamalainen/antarctic-database-go/-/badge.svg)](https://sourcegraph.com/github.com/carlohamalainen/antarctic-database-go?badge)

antarctic-database-go provides a simple API for querying documents and measures in the [ATS Database][atsdb].
Specifically, we support the _Antarctic Treaty Database_ and _Meeting Documents_ databases.

[Antarctic Treaty Database search page][treatydb] description:

> In this database you can find the text of measures adopted by the ATCM (including all Recommendations, Measures, Decisions and Resolutions) from 1961 to now together with their attachments and information on their legal status.
>
> This database contains the text of all Recommendations, Measures, Decisions and Resolutions and other measures adopted by the ATCM together with their attachments and information on their legal status.

[Meeting Documents Archive search page][meetingdb] description:

> A searchable database of all the working documents submitted by Parties, Observers and Experts to the meetings.

## Table of Contents

* [Installation](#installation)
* [Changelog](#changelog)
* [API](#api)
* [Examples](#examples)
* [Related Projects](#related-projects)
* [TODO](#TODO)
* [Support](#support)
* [License](#license)

## Installation

Starting with `v1.0.0` of antarctic-database-go, Go 1.23+ is required.

    $ go get github.com/carlohamalainen/antarctic-database-go

(optional) To run unit tests:

    $ cd $GOPATH/src/github.com/carlohamalainen/antarctic-database-go
    $ go test

## Changelog

*    **2024-09-09 (v1.0.3)** : Tag version 1.0.3.
*    **2024-09-10** : Documentation, tidyup.
*    **2024-09-09 (v1.0.2)** : Tag version 1.0.2.
*    **2024-09-09** : Documentation, tidyup.
*    **2024-09-07 (v1.0.1)** : Tag version 1.0.1.
*    **2024-09-07** Change package name from `api` to `ats`.
*    **2024-09-07 (v1.0.0)** : Tag version 1.0.0.
*    **2024-09-07** Initial commit.

## API

The complete [package reference documentation can be found here][doc].

[api.go](./api.go) has the main API. Use this for constructing URLs to the treaties, measures (recommendations), and meeting documents.

[metadata.go](./metadata.go) is an auto-generated module with metadata for searching. For example it defines constants for meetings:

```go
type Meeting_Date string

const (
	Meeting_Date_ATCM_46_CEP_26_Kochi_2024                Meeting_Date = "05/30/2024"
	Meeting_Date_ATCM_III_Brussels_1964                   Meeting_Date = "06/13/1964"
	Meeting_Date_ATCM_II_Buenos_Aires_1962                Meeting_Date = "07/28/1962"
	Meeting_Date_ATCM_IV_Santiago_1966                    Meeting_Date = "11/18/1966"
	Meeting_Date_ATCM_IX_London_1977                      Meeting_Date = "10/07/1977"
	Meeting_Date_ATCM_I_Canberra_1961                     Meeting_Date = "07/24/1961"
  // many more lines...
)
```

and a list of all meetings:

```go
var Meeting_DateKeys []Meeting_Date = []Meeting_Date{Meeting_Date_ATCM_46_CEP_26_Kochi_2024, Meeting_Date_ATCM_III_Brussels_1964, Meeting_Date_ATCM_II_Buenos_Aires_1962,
  // many more lines
}
```
and a convenient function to show a meeting:

```go
func Meeting_DateToString(m Meeting_Date) string {
	switch m {
	case Meeting_Date_ATCM_46_CEP_26_Kochi_2024:
		return "Meeting_Date_ATCM_46_CEP_26_Kochi_2024"
	case Meeting_Date_ATCM_III_Brussels_1964:
		return "Meeting_Date_ATCM_III_Brussels_1964"
  // many more lines
```

[structs.go](./structs.go) has auto-generated structures for json responses. For example, meeting document responses can be unmarshalled
to a `Document` which has a pager and a payload. Pages start at `1` and a `DocumentPager.Next` of 0 indicates the final page.

```go
type Document struct {
	Pager   DocumentPager         `json:"pager"`
	Payload []DocumentPayloadItem `json:"payload"`
}

type DocumentPager struct {
	Lastpage int                      `json:"lastPage"`
	Next     int                      `json:"next"`
	Page     int                      `json:"page"`
	Pages    []DocumentPagerPagesItem `json:"pages"`
	Perpage  int                      `json:"perPage"`
	Prev     int                      `json:"prev"`
	Total    int                      `json:"total"`
}

type DocumentPayloadItem struct {
	Abbreviation   string                               `json:"Abbreviation"`
	Acronym_en     string                               `json:"Acronym_en"`
	Agendas        []DocumentPayloadItemAgendasItem     `json:"Agendas"`
	Attachments    []DocumentPayloadItemAttachmentsItem `json:"Attachments"`
	Isbusy         bool                                 `json:"IsBusy"`
	Isselfbusy     bool                                 `json:"IsSelfBusy"`
	Meeting_city   string                               `json:"Meeting_city"`
	Meeting_id     int                                  `json:"Meeting_id"`
	Meeting_name   string                               `json:"Meeting_name"`
	Meeting_number string                               `json:"Meeting_number"`
	Meeting_type   string                               `json:"Meeting_type"`
	Meeting_year   int                                  `json:"Meeting_year"`
	Name           string                               `json:"Name"`
	Number         int                                  `json:"Number"`
	Pap_type_id    int                                  `json:"Pap_type_id"`
	Paper_id       int                                  `json:"Paper_id"`
	Parties        []DocumentPayloadItemPartiesItem     `json:"Parties"`
	Revision       int                                  `json:"Revision"`
	State_en       int                                  `json:"State_en"`
	State_fr       int                                  `json:"State_fr"`
	State_ru       int                                  `json:"State_ru"`
	State_sp       int                                  `json:"State_sp"`
	Type           string                               `json:"Type"`
}
```

## Examples

[example/measure](./example/measure/): search and parse measures.

[example/downloads](./example/downloads/): search and validate meeting documents

[example/csv](./example/csv/): search meeting documents and produce simple CSV and Parquet output.

Use [duckdb](duckdb) to have a quick look at the Parquet output:

```
D FROM 'meetings.parquet';
┌─────────────┬────────┬──────────┬──────────────────────┬─────────────┬───────────┬─────────────┬────────────────────┬──────────────────────┐
│ MeetingName │ Number │ Revision │         Name         │ MeetingCity │ MeetingID │ MeetingYear │      Agendas       │       Parties        │
│   varchar   │ int64  │  int64   │       varchar        │   varchar   │   int64   │    int64    │     varchar[]      │      varchar[]       │
├─────────────┼────────┼──────────┼──────────────────────┼─────────────┼───────────┼─────────────┼────────────────────┼──────────────────────┤
│ ATCM XII    │      1 │        1 │ Agreed measures fo…  │ Canberra    │        24 │        1983 │ [ATCM 6]           │ [United Kingdom]     │
│ ATCM XII    │      2 │        0 │ Agenda               │ Canberra    │        24 │        1983 │ [ATCM 4]           │ [Australia]          │
│ ATCM XII    │      3 │        0 │ Improvement of tel…  │ Canberra    │        24 │        1983 │ [ATCM 5]           │ [United Kingdom]     │
│ ATCM XII    │      4 │        1 │ Non-governmental e…  │ Canberra    │        24 │        1983 │ [ATCM 8]           │ [United Kingdom]     │
│ ATCM XII    │      5 │        0 │ Telecommunications…  │ Canberra    │        24 │        1983 │ [ATCM 5]           │ [Australia]          │
│ ATCM XII    │      6 │        0 │ Discussion paper o…  │ Canberra    │        24 │        1983 │ [ATCM 6]           │ [Australia]          │
│ ATCM XII    │      7 │        0 │ Draft recommendati…  │ Canberra    │        24 │        1983 │ [ATCM 6]           │ [Australia]          │
│ ATCM XII    │      8 │        0 │ Operation of the A…  │ Canberra    │        24 │        1983 │ [ATCM 10]          │ [Chile]              │
│ ATCM XII    │      9 │        0 │ Working document o…  │ Canberra    │        24 │        1983 │ [ATCM 6]           │ [Argentina, Chile]   │
│ ATCM XII    │     10 │        0 │ Public availabilit…  │ Canberra    │        24 │        1983 │ [ATCM 11]          │ [United Kingdom]     │
│ ATCM XII    │     11 │        0 │ Discussion paper o…  │ Canberra    │        24 │        1983 │ [ATCM 12]          │ [United Kingdom]     │
│ ATCM XII    │     12 │        0 │ Note on Exchange o…  │ Canberra    │        24 │        1983 │ [ATCM 13]          │ [United Kingdom]     │
│ ATCM XII    │     13 │        0 │ Exchanges of infor…  │ Canberra    │        24 │        1983 │ [ATCM 13]          │ [United Kingdom]     │
│ ATCM XII    │     14 │        1 │ Draft recommendati…  │ Canberra    │        24 │        1983 │ [ATCM 7]           │ [Argentina, Chile,…  │
│ ATCM XII    │     15 │        0 │ Improvement of tel…  │ Canberra    │        24 │        1983 │ [ATCM 5]           │ [Argentina, Brazil…  │
│ ATCM XII    │     16 │        1 │ Draft recommendati…  │ Canberra    │        24 │        1983 │ [ATCM 17]          │ [India]              │
│ ATCM XII    │     17 │        1 │ Extension of the e…  │ Canberra    │        24 │        1983 │ [ATCM 7]           │ [United States]      │
│ ATCM XII    │     18 │        0 │ Draft recommendati…  │ Canberra    │        24 │        1983 │ [ATCM 6]           │ [Australia]          │
│ ATCM XII    │     19 │        0 │ Item relating to t…  │ Canberra    │        24 │        1983 │ [ATCM 9]           │ [Argentina]          │
│ ATCM XII    │     20 │        0 │ Item relating to g…  │ Canberra    │        24 │        1983 │ [ATCM 9]           │ [Argentina]          │
│ ATCM XII    │     21 │        0 │ Draft recommendati…  │ Canberra    │        24 │        1983 │ [ATCM 12]          │ [United Kingdom]     │
│ ATCM XII    │     22 │        0 │ Draft recommendati…  │ Canberra    │        24 │        1983 │ [ATCM 5]           │ [Australia]          │
│ ATCM XII    │     23 │        0 │ Draft recommendati…  │ Canberra    │        24 │        1983 │ [ATCM 5]           │ [Australia]          │
│ ATCM XII    │     24 │        1 │ Final report of th…  │ Canberra    │        24 │        1983 │ [ATCM 18]          │ [Australia]          │
│ ATCM XII    │     24 │        2 │ Final report of th…  │ Canberra    │        24 │        1983 │ [ATCM 18]          │ [Australia]          │
│ ATCM XII    │     25 │        0 │ Draft recommendati…  │ Canberra    │        24 │        1983 │ [ATCM 6]           │ [Australia]          │
│ ATCM XII    │     26 │        0 │ Recommendation on …  │ Canberra    │        24 │        1983 │ [ATCM 10, ATCM 11] │ [Australia]          │
│ ATCM XII    │     27 │        1 │ Message from the T…  │ Canberra    │        24 │        1983 │ [ATCM 17]          │ [Australia]          │
│ ATCM XII    │     28 │        1 │ SCAR assistance to…  │ Canberra    │        24 │        1983 │ [ATCM 10]          │ [Australia]          │
├─────────────┴────────┴──────────┴──────────────────────┴─────────────┴───────────┴─────────────┴────────────────────┴──────────────────────┤
│ 29 rows                                                                                                                          9 columns │
└────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────┘
```


## Related Projects

- [ats-papers](https://github.com/gcuth/ats-papers), a Python pipeline for scraping and analysing Antarctic Treaty Papers. Archived repository.

## TODO

Try [Dave's dst](https://github.com/dave/dst) for code gen - avoid the manual
``buf.WriteString("// AUTOGENERATED FILE! Do not edit!\n\n");``
and perhaps try to add godoc for the autogenerated constants, etc.

## Support

There are a number of ways you can support the project:

* Use it, star it, build something with it, spread the word!
  - If you do build something open-source or otherwise publicly-visible, let me know so I can add it to the [Related Projects](#related-projects) section!
* Raise issues to improve the project (note: doc typos and clarifications are issues too!)
  - Please search existing issues before opening a new one - it may have already been addressed.
* Pull requests: please discuss new code in an issue first, unless the fix is really trivial.
  - Make sure new code is tested.
  - Be mindful of existing code - PRs that break existing code have a high probability of being declined, unless it fixes a serious issue.

## License

The [BSD 3-Clause license][bsd], the same as the [Go language][golic].

[atsdb]: https://www.ats.aq/e/tools-and-resources.html
[bsd]: https://opensource.org/licenses/BSD-3-Clause
[doc]: https://pkg.go.dev/github.com/carlohamalainen/antarctic-database-go
[duckdb]: https://duckdb.org
[golic]: https://go.dev/LICENSE
[treatydb]: https://www.ats.aq/devAS/ToolsAndResources/AntarcticTreatyDatabase?lang=e
[meetingdb]: https://www.ats.aq/devAS/Meetings/DocDatabase?lang=e
