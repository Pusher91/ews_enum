package ews

// Contact represents a resolved GAL entry.
type Contact struct {
	DisplayName  string `json:"display_name" xml:"DisplayName"`
	EmailAddress string `json:"email_address"`
	Title        string `json:"title,omitempty" xml:"Title"`
	Department   string `json:"department,omitempty" xml:"Department"`
	Office       string `json:"office,omitempty" xml:"OfficeLocation"`
	Phone        string `json:"phone,omitempty" xml:"PhoneNumber"`
	Company      string `json:"company,omitempty" xml:"CompanyName"`
	GivenName    string `json:"given_name,omitempty" xml:"GivenName"`
	Surname      string `json:"surname,omitempty" xml:"Surname"`
}
