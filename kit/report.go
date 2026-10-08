package kit

import (
	"encoding/xml"
	"fmt"
	"io"
)

type suite struct {
	XMLName  xml.Name   `xml:"testsuite"`
	Name     string     `xml:"name,attr"`
	Tests    int        `xml:"tests,attr"`
	Failures int        `xml:"failures,attr"`
	Skipped  int        `xml:"skipped,attr"`
	Cases    []testCase `xml:"testcase"`
}
type testCase struct {
	Name    string   `xml:"name,attr"`
	Class   string   `xml:"classname,attr"`
	Time    string   `xml:"time,attr"`
	Failure *failure `xml:"failure,omitempty"`
	Skipped *failure `xml:"skipped,omitempty"`
}
type failure struct {
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}

func (r Report) WriteJUnit(w io.Writer) error {
	s := suite{Name: "lifecase", Tests: len(r.Results)}
	for _, v := range r.Results {
		c := testCase{Name: v.Scenario, Class: "lifecycle." + r.OS, Time: fmt.Sprintf("%.3f", float64(v.DurationMS)/1000)}
		if v.Status == "fail" {
			s.Failures++
			f := failure{Message: "lifecycle contract failed"}
			for _, a := range v.Assertions {
				if !a.Pass {
					f.Text += a.Name + ": " + a.Detail + "\n"
				}
			}
			c.Failure = &f
		}
		if v.Status == "unsupported" {
			s.Skipped++
			c.Skipped = &failure{Message: "capability unsupported"}
		}
		s.Cases = append(s.Cases, c)
	}
	e := xml.NewEncoder(w)
	e.Indent("", "  ")
	return e.Encode(s)
}
