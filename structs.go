// AUTOGENERATED FILE! Do not edit!

package api

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
type DocumentPagerPagesItem struct {
	Active  bool `json:"active"`
	Enabled bool `json:"enabled"`
	Value   int  `json:"value"`
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
type DocumentPayloadItemAgendasItem struct {
	Agenda_id  int    `json:"Agenda_id"`
	Isbusy     bool   `json:"IsBusy"`
	Isselfbusy bool   `json:"IsSelfBusy"`
	Number     string `json:"Number"`
	Paper_id   int    `json:"Paper_id"`
}
type DocumentPayloadItemAttachmentsItem struct {
	Att_lang       string `json:"Att_lang"`
	Attachment_id  int    `json:"Attachment_id"`
	Isbusy         bool   `json:"IsBusy"`
	Isselfbusy     bool   `json:"IsSelfBusy"`
	Meeting_number string `json:"Meeting_number"`
	Meeting_type   string `json:"Meeting_type"`
	Name           string `json:"Name"`
	Number         int    `json:"Number"`
	Paper_id       int    `json:"Paper_id"`
	Revision       int    `json:"Revision"`
	Type           string `json:"Type"`
}
type DocumentPayloadItemPartiesItem struct {
	Isbusy     bool   `json:"IsBusy"`
	Isselfbusy bool   `json:"IsSelfBusy"`
	Name       string `json:"Name"`
	Paper_id   int    `json:"Paper_id"`
	Party_id   int    `json:"Party_id"`
	Primary    int    `json:"Primary"`
}
type Treaty struct {
	Pager   TreatyPager         `json:"pager"`
	Payload []TreatyPayloadItem `json:"payload"`
}
type TreatyPager struct {
	Lastpage int                    `json:"lastPage"`
	Next     int                    `json:"next"`
	Page     int                    `json:"page"`
	Pages    []TreatyPagerPagesItem `json:"pages"`
	Perpage  int                    `json:"perPage"`
	Prev     int                    `json:"prev"`
	Total    int                    `json:"total"`
}
type TreatyPagerPagesItem struct {
	Active  bool `json:"active"`
	Enabled bool `json:"enabled"`
	Value   int  `json:"value"`
}
type TreatyPayloadItem struct {
	Aatmid                 int    `json:"AATMID"`
	Arecid                 int    `json:"ARecID"`
	Hasobsoleteattachments bool   `json:"HasObsoleteAttachments"`
	Irecno                 string `json:"IRecNo"`
	Isbusy                 bool   `json:"IsBusy"`
	Isselfbusy             bool   `json:"IsSelfBusy"`
	Msubject               string `json:"MSubject"`
	Obsolete_type_id       int    `json:"Obsolete_type_id"`
	Satcmcity              string `json:"SATCMCity"`
	Satcmno                string `json:"SATCMNo"`
	Yearmeeting            int    `json:"YearMeeting"`
}