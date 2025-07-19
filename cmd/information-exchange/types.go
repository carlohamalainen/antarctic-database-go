package main

import (
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/carlohamalainen/antarctic-database-go"
)

type Report struct {
	Country string
	English []ats.URL
	Spanish []ats.URL
	French  []ats.URL
	Russian []ats.URL
}

type ReportCollection struct {
	Annual    []Report
	PreSeason []Report
}

func (reportCollection *ReportCollection) getReports(period Period) []Report {
	switch period {
	case ReportPeriodPreSeason:
		return reportCollection.PreSeason
	case ReportPeriodAnnual:
		return reportCollection.Annual
	default:
		panic("unknown report period: " + period)
	}
}

type SectionType string

const (
	SectionPublished       SectionType = "PUBLISHED"
	SectionNothingToReport SectionType = "NOTHING_TO_REPORT"
	SectionNotYetPublished SectionType = "NOT_YET_PUBLISHED"
)

type Section struct {
	Title    string // e.g. top level accordion box, e.g. Operational Information - Non Governmental Expeditions - Vessel-Based Operations - Report
	ReportID string
	Type     SectionType
	Chunks   []Chunk
	RawBody  string
}

type ValueData struct {
	RawHTML string
	Text    string

	Location []LocationP
}

type Chunk struct {
	Id         string
	MainHeader map[string]ValueData // e.g. operator, name, number of voyages, maximum crew
	Visited    []Visit
	Flights    []Flight
}

type Visit struct {
	Header map[string]string   // e.g. DEPART. DATE, ARRIVAL PORT, EXPEDITION LEADER, ...
	Visits []map[string]string // e.g.  Site Name:  Wild, cabo  Lat:  61º 06´ 00´´ S  Long:  54º 52´ 00´´ W
	//       Visit Date:  27/10/2024
	//       This visit includes landing:  No
}

type Flight struct {
	Header map[string]string // e.g. FIXME // this isn't named the best, same in `Visit`

	// no sub flight info here, just a basic table

}

// Language = English | Spanish | French | Russian
type Language string

const (
	LanguageEnglish Language = "English"
	LanguageSpanish Language = "Spanish"
	LanguageFrench  Language = "French"
	LanguageRussian Language = "Russian"
)

func Languages() []Language {
	return []Language{LanguageEnglish, LanguageSpanish, LanguageFrench, LanguageRussian}
}

// Period = Annual | PreSeason
type Period string

const (
	ReportPeriodAnnual    Period = "Annual"
	ReportPeriodPreSeason Period = "PreSeason"
)

type ExpeditionData struct {
	VisitedSites []map[string]string
}

type Expanded struct {
	Period           Period
	Country          string
	Language         Language
	URL              ats.URL
	Document         string
	GoqueryDocument  *goquery.Document
	GetRptEndpointFn string
	Sections         []Section
}

// LocationRecord is our flattened row for Parquet.
type LocationRecord struct {
	URL          string `parquet:"url,required"`
	Period       string `parquet:"period,required"`
	Country      string `parquet:"country,required"`
	Language     string `parquet:"language,required"`
	SectionTitle string `parquet:"section_title,required"`
	ReportID     string `parquet:"report_id,required"`
	Key          string `parquet:"data_key,required"`
	TextValue    string `parquet:"text_value,required"`

	// From LocationP
	SiteName     string `parquet:"site_name,optional"`
	LatitudeRaw  string `parquet:"latitude_raw,optional"`
	LongitudeRaw string `parquet:"longitude_raw,optional"`

	Latitude  float64 `parquet:"latitude,optional"`
	Longitude float64 `parquet:"longitude,optional"`
}

type ExpeditionRecord struct {
	URL          string `parquet:"url,required"`
	Period       string `parquet:"period,required"`
	Country      string `parquet:"country,required"`
	Language     string `parquet:"language,required"`
	SectionTitle string `parquet:"section_title,required"`
	ReportID     string `parquet:"report_id,required"`

	Expedition_accommodation_capacity                              string `parquet:"expedition_accommodation_capacity,optional"`
	Expedition_actions_taken                                       string `parquet:"expedition_actions_taken,optional"`
	Expedition_activities                                          string `parquet:"expedition_activities,optional"`
	Expedition_activity                                            string `parquet:"expedition_activity,optional"`
	Expedition_aircraft_additional_information                     string `parquet:"expedition_aircraft_additional_information,optional"`
	Expedition_asma                                                string `parquet:"expedition_asma,optional"`
	Expedition_aspa                                                string `parquet:"expedition_aspa,optional"`
	Expedition_comments_received                                   string `parquet:"expedition_comments_received,optional"`
	Expedition_contact_address                                     string `parquet:"expedition_contact_address,optional"`
	Expedition_contact_point                                       string `parquet:"expedition_contact_point,optional"`
	Expedition_country_of_registry                                 string `parquet:"expedition_country_of_registry,optional"`
	Expedition_date_begin                                          string `parquet:"expedition_date_begin,optional"`
	Expedition_date_end                                            string `parquet:"expedition_date_end,optional"`
	Expedition_date_of_effect                                      string `parquet:"expedition_date_of_effect,optional"`
	Expedition_date_period_frequency                               string `parquet:"expedition_date_period_frequency,optional"`
	Expedition_decision_comment                                    string `parquet:"expedition_decision_comment,optional"`
	Expedition_description                                         string `parquet:"expedition_description,optional"`
	Expedition_description_of_measure                              string `parquet:"expedition_description_of_measure,optional"`
	Expedition_description_of_measures_taken                       string `parquet:"expedition_description_of_measures_taken,optional"`
	Expedition_direction                                           string `parquet:"expedition_direction,optional"`
	Expedition_email                                               string `parquet:"expedition_email,optional"`
	Expedition_email_address                                       string `parquet:"expedition_email_address,optional"`
	Expedition_event_or_project_name_number                        string `parquet:"expedition_event_or_project_name_number,optional"`
	Expedition_expedition_name                                     string `parquet:"expedition_expedition_name,optional"`
	Expedition_file                                                string `parquet:"expedition_file,optional"`
	Expedition_fixed_site_field_camp_ship                          string `parquet:"expedition_fixed_site_field_camp_ship,optional"`
	Expedition_hsm                                                 string `parquet:"expedition_hsm,optional"`
	Expedition_ice_strength                                        string `parquet:"expedition_ice_strength,optional"`
	Expedition_impact_area                                         string `parquet:"expedition_impact_area,optional"`
	Expedition_implementation_report                               string `parquet:"expedition_implementation_report,optional"`
	Expedition_information_obtained                                string `parquet:"expedition_information_obtained,optional"`
	Expedition_job_title_or_position                               string `parquet:"expedition_job_title_or_position,optional"`
	Expedition_launch                                              string `parquet:"expedition_launch,optional"`
	Expedition_link_url                                            string `parquet:"expedition_link_url,optional"`
	Expedition_location                                            string `parquet:"expedition_location,optional"`
	Expedition_location_launch                                     string `parquet:"expedition_location_launch,optional"`
	Expedition_locations                                           string `parquet:"expedition_locations,optional"`
	Expedition_max_altitude                                        string `parquet:"expedition_max_altitude,optional"`
	Expedition_maximum_crew                                        string `parquet:"expedition_maximum_crew,optional"`
	Expedition_maximum_passengers                                  string `parquet:"expedition_maximum_passengers,optional"`
	Expedition_maximum_population                                  string `parquet:"expedition_maximum_population,optional"`
	Expedition_medical_facilities                                  string `parquet:"expedition_medical_facilities,optional"`
	Expedition_method_of_transportation_to_within_from_antarctica  string `parquet:"expedition_method_of_transportation_to_within_from_antarctica,optional"`
	Expedition_military_personnel_in_expeditions                   string `parquet:"expedition_military_personnel_in_expeditions,optional"`
	Expedition_name                                                string `parquet:"expedition_name,optional"`
	Expedition_name_of_activity                                    string `parquet:"expedition_name_of_activity,optional"`
	Expedition_number_and_type_of_armaments_possessed_by_personnel string `parquet:"expedition_number_and_type_of_armaments_possessed_by_personnel,optional"`
	Expedition_number_of_flights                                   string `parquet:"expedition_number_of_flights,optional"`
	Expedition_number_of_participants                              string `parquet:"expedition_number_of_participants,optional"`
	Expedition_number_of_people                                    string `parquet:"expedition_number_of_people,optional"`
	Expedition_number_of_personnel                                 string `parquet:"expedition_number_of_personnel,optional"`
	Expedition_number_of_voyages                                   string `parquet:"expedition_number_of_voyages,optional"`
	Expedition_objective                                           string `parquet:"expedition_objective,optional"`
	Expedition_official_name                                       string `parquet:"expedition_official_name,optional"`
	Expedition_operating_period                                    string `parquet:"expedition_operating_period,optional"`
	Expedition_operator_contact_address                            string `parquet:"expedition_operator_contact_address,optional"`
	Expedition_operator_email_address                              string `parquet:"expedition_operator_email_address,optional"`
	Expedition_operator_name                                       string `parquet:"expedition_operator_name,optional"`
	Expedition_operator_website_address                            string `parquet:"expedition_operator_website_address,optional"`
	Expedition_organisation                                        string `parquet:"expedition_organisation,optional"`
	Expedition_organizations_responsible                           string `parquet:"expedition_organizations_responsible,optional"`
	Expedition_period_from                                         string `parquet:"expedition_period_from,optional"`
	Expedition_period_length_of_the_activity                       string `parquet:"expedition_period_length_of_the_activity,optional"`
	Expedition_period_to                                           string `parquet:"expedition_period_to,optional"`
	Expedition_permit_number                                       string `parquet:"expedition_permit_number,optional"`
	Expedition_permit_period                                       string `parquet:"expedition_permit_period,optional"`
	Expedition_phone                                               string `parquet:"expedition_phone,optional"`
	Expedition_planned_date_of_first_flight                        string `parquet:"expedition_planned_date_of_first_flight,optional"`
	Expedition_planned_date_of_last_flight                         string `parquet:"expedition_planned_date_of_last_flight,optional"`
	Expedition_procedures_put_in_place                             string `parquet:"expedition_procedures_put_in_place,optional"`
	Expedition_project_title_number                                string `parquet:"expedition_project_title_number,optional"`
	Expedition_purpose                                             string `parquet:"expedition_purpose,optional"`
	Expedition_related_eia                                         string `parquet:"expedition_related_eia,optional"`
	Expedition_remarks                                             string `parquet:"expedition_remarks,optional"`
	Expedition_remarks_description                                 string `parquet:"expedition_remarks_description,optional"`
	Expedition_routes                                              string `parquet:"expedition_routes,optional"`
	Expedition_scope_coverage_of_the_plan                          string `parquet:"expedition_scope_coverage_of_the_plan,optional"`
	Expedition_season_dates                                        string `parquet:"expedition_season_dates,optional"`
	Expedition_seasonality                                         string `parquet:"expedition_seasonality,optional"`
	Expedition_specifications                                      string `parquet:"expedition_specifications,optional"`
	Expedition_status                                              string `parquet:"expedition_status,optional"`
	Expedition_summary_of_activities                               string `parquet:"expedition_summary_of_activities,optional"`
	Expedition_title                                               string `parquet:"expedition_title,optional"`
	Expedition_title_of_measure                                    string `parquet:"expedition_title_of_measure,optional"`
	Expedition_topics                                              string `parquet:"expedition_topics,optional"`
	Expedition_type                                                string `parquet:"expedition_type,optional"`
	Expedition_website_address                                     string `parquet:"expedition_website_address,optional"`
	Visit_areas_of_operation                                       string `parquet:"visit_areas_of_operation,optional"`
	Visit_arrival_date                                             string `parquet:"visit_arrival_date,optional"`
	Visit_arrival_location                                         string `parquet:"visit_arrival_location,optional"`
	Visit_arrival_port                                             string `parquet:"visit_arrival_port,optional"`
	Visit_assistance_received                                      string `parquet:"visit_assistance_received,optional"`
	Visit_combined_activity                                        string `parquet:"visit_combined_activity,optional"`
	Visit_date_of_incident                                         string `parquet:"visit_date_of_incident,optional"`
	Visit_depart_date                                              string `parquet:"visit_depart_date,optional"`
	Visit_depart_port                                              string `parquet:"visit_depart_port,optional"`
	Visit_departure_date                                           string `parquet:"visit_departure_date,optional"`
	Visit_departure_location                                       string `parquet:"visit_departure_location,optional"`
	Visit_expedition_leader                                        string `parquet:"visit_expedition_leader,optional"`
	Visit_location_of_activities                                   string `parquet:"visit_location_of_activities,optional"`
	Visit_number_of_crew_members                                   string `parquet:"visit_number_of_crew_members,optional"`
	Visit_number_of_passengers                                     string `parquet:"visit_number_of_passengers,optional"`
	Visit_purpose                                                  string `parquet:"visit_purpose,optional"`
	Visit_route                                                    string `parquet:"visit_route,optional"`
	Visit_site_name                                                string `parquet:"visit_site_name,optional"`
	Visit_summary_description                                      string `parquet:"visit_summary_description,optional"`
	Visit_tc                                                       string `parquet:"visit_tc,optional"`
	Visit_tp                                                       string `parquet:"visit_tp,optional"`
	Visit_type_of_unusual_incident_occurred                        string `parquet:"visit_type_of_unusual_incident_occurred,optional"`
}

type VisitRecord struct {
	URL          string `parquet:"url,required"`
	Period       string `parquet:"period,required"`
	Country      string `parquet:"country,required"`
	Language     string `parquet:"language,required"`
	SectionTitle string `parquet:"section_title,required"`
	ReportID     string `parquet:"report_id,required"`

	// Some vessel info
	VesselName              string `parquet:"vessel_name,optional"`
	VesselCountryOfRegistry string `parquet:"vessel_country_of_registry,optional"`
	VesselMaxCrew           string `parquet:"vessel_max_crew,optional"`
	VesselNumberOfVoyages   string `parquet:"vessel_number_of_voyages,optional"`

	// Chunk ID
	ChunkId string `parquet:"chunk_id,required"`

	// Operator info
	OperatorName           string `parquet:"operator_name,optional"`
	OperatorWebsiteAddress string `parquet:"operator_website_address,optional"`
	OperatorContactAddress string `parquet:"operator_contact_address,optional"`

	// Visits
	VisitDepartDate       time.Time `parquet:"visit_depart_date,optional"`
	VisitDepartPort       string    `parquet:"visit_depart_port,optional"`
	VisitArrivalDate      time.Time `parquet:"visit_arrival_date,optional"`
	VisitArrivalPort      string    `parquet:"visit_arrival_port,optional"`
	VisitExpeditionLeader string    `parquet:"visit_expedition_leader,optional"`

	VisitLatitude            float64   `parquet:"visit_latitude,optional"`
	VisitLongitude           float64   `parquet:"visit_longitude,optional"`
	VisitDate                time.Time `parquet:"visit_date,optional"`
	ThisVisitIncludesLanding string    `parquet:"this_visit_includes_landing,optional"`
	VisitSiteName            string    `parquet:"visit_site_name,optional"`
}

type FlightRecord struct {
	URL          string `parquet:"url,optional"`
	Period       string `parquet:"period,optional"`
	Country      string `parquet:"country,optional"`
	Language     string `parquet:"language,optional"`
	SectionTitle string `parquet:"section_title,optional"`
	ReportID     string `parquet:"report_id,optional"`

	TableID string `parquet:"table_id,optional"`

	DepartureDate          time.Time `parquet:"departure_date,optional"`
	Route                  string    `parquet:"route,optional"`
	Purpose                string    `parquet:"purpose,optional"`
	Type                   string    `parquet:"type,optional"`
	MaximumCrew            string    `parquet:"maximum_crew,optional"`
	MaximumPassengers      string    `parquet:"maximum_passengers,optional"`
	NumberOfFlights        string    `parquet:"number_of_flights,optional"`
	PlannedDateFirstFlight string    `parquet:"planned_date_first_flight,optional"`
	PlannedDateLastFlight  string    `parquet:"planned_date_last_flight,optional"`
	AircraftAdditionalInfo string    `parquet:"aircraft_additional_info,optional"`
}
