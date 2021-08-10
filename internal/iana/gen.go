// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build ignore
// +build ignore

//go:generate go run gen.go

// This program generates internet protocol constants and tables by
// reading IANA protocol registries.
package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"go/format"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"strings"
)

var registries = []struct {
	url   string
	parse func(io.Writer, io.Reader) error
}{
	{
		"https://www.iana.org/assignments/dscp-registry/dscp-registry.xml",
		parseDSCPRegistry,
	},
	{
		"https://www.iana.org/assignments/protocol-numbers/protocol-numbers.xml",
		parseProtocolNumbers,
	},
	{
		"https://www.iana.org/assignments/address-family-numbers/address-family-numbers.xml",
		parseAddrFamilyNumbers,
	},
}

func main() {
	var bb bytes.Buffer
	fmt.Fprintf(&bb, "// go generate gen.go\n")
	fmt.Fprintf(&bb, "// Code generated by the command above; DO NOT EDIT.\n\n")
	fmt.Fprintf(&bb, "// Package iana provides protocol number resources managed by the Internet Assigned Numbers Authority (IANA).\n")
	fmt.Fprintf(&bb, `package iana // import "github.com/tailscale/net/internal/iana"`+"\n\n")
	for _, r := range registries {
		resp, err := http.Get(r.url)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			fmt.Fprintf(os.Stderr, "got HTTP status code %v for %v\n", resp.StatusCode, r.url)
			os.Exit(1)
		}
		if err := r.parse(&bb, resp.Body); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Fprintf(&bb, "\n")
	}
	b, err := format.Source(bb.Bytes())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := ioutil.WriteFile("const.go", b, 0644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func parseDSCPRegistry(w io.Writer, r io.Reader) error {
	dec := xml.NewDecoder(r)
	var dr dscpRegistry
	if err := dec.Decode(&dr); err != nil {
		return err
	}
	fmt.Fprintf(w, "// %s, Updated: %s\n", dr.Title, dr.Updated)
	fmt.Fprintf(w, "const (\n")
	for _, dr := range dr.escapeDSCP() {
		fmt.Fprintf(w, "DiffServ%s = %#02x", dr.Name, dr.Value)
		fmt.Fprintf(w, "// %s\n", dr.OrigName)
	}
	for _, er := range dr.escapeECN() {
		fmt.Fprintf(w, "%s = %#02x", er.Descr, er.Value)
		fmt.Fprintf(w, "// %s\n", er.OrigDescr)
	}
	fmt.Fprintf(w, ")\n")
	return nil
}

type dscpRegistry struct {
	XMLName    xml.Name `xml:"registry"`
	Title      string   `xml:"title"`
	Updated    string   `xml:"updated"`
	Note       string   `xml:"note"`
	Registries []struct {
		Title      string `xml:"title"`
		Registries []struct {
			Title   string `xml:"title"`
			Records []struct {
				Name  string `xml:"name"`
				Space string `xml:"space"`
			} `xml:"record"`
		} `xml:"registry"`
		Records []struct {
			Value string `xml:"value"`
			Descr string `xml:"description"`
		} `xml:"record"`
	} `xml:"registry"`
}

type canonDSCPRecord struct {
	OrigName string
	Name     string
	Value    int
}

func (drr *dscpRegistry) escapeDSCP() []canonDSCPRecord {
	var drs []canonDSCPRecord
	for _, preg := range drr.Registries {
		if !strings.Contains(preg.Title, "Differentiated Services Field Codepoints") {
			continue
		}
		for _, reg := range preg.Registries {
			if !strings.Contains(reg.Title, "Pool 1 Codepoints") {
				continue
			}
			drs = make([]canonDSCPRecord, len(reg.Records))
			sr := strings.NewReplacer(
				"+", "",
				"-", "",
				"/", "",
				".", "",
				" ", "",
			)
			for i, dr := range reg.Records {
				s := strings.TrimSpace(dr.Name)
				drs[i].OrigName = s
				drs[i].Name = sr.Replace(s)
				n, err := strconv.ParseUint(dr.Space, 2, 8)
				if err != nil {
					continue
				}
				drs[i].Value = int(n) << 2
			}
		}
	}
	return drs
}

type canonECNRecord struct {
	OrigDescr string
	Descr     string
	Value     int
}

func (drr *dscpRegistry) escapeECN() []canonECNRecord {
	var ers []canonECNRecord
	for _, reg := range drr.Registries {
		if !strings.Contains(reg.Title, "ECN Field") {
			continue
		}
		ers = make([]canonECNRecord, len(reg.Records))
		sr := strings.NewReplacer(
			"Capable", "",
			"Not-ECT", "",
			"ECT(1)", "",
			"ECT(0)", "",
			"CE", "",
			"(", "",
			")", "",
			"+", "",
			"-", "",
			"/", "",
			".", "",
			" ", "",
		)
		for i, er := range reg.Records {
			s := strings.TrimSpace(er.Descr)
			ers[i].OrigDescr = s
			ss := strings.Split(s, " ")
			if len(ss) > 1 {
				ers[i].Descr = strings.Join(ss[1:], " ")
			} else {
				ers[i].Descr = ss[0]
			}
			ers[i].Descr = sr.Replace(er.Descr)
			n, err := strconv.ParseUint(er.Value, 2, 8)
			if err != nil {
				continue
			}
			ers[i].Value = int(n)
		}
	}
	return ers
}

func parseProtocolNumbers(w io.Writer, r io.Reader) error {
	dec := xml.NewDecoder(r)
	var pn protocolNumbers
	if err := dec.Decode(&pn); err != nil {
		return err
	}
	prs := pn.escape()
	prs = append([]canonProtocolRecord{{
		Name:  "IP",
		Descr: "IPv4 encapsulation, pseudo protocol number",
		Value: 0,
	}}, prs...)
	fmt.Fprintf(w, "// %s, Updated: %s\n", pn.Title, pn.Updated)
	fmt.Fprintf(w, "const (\n")
	for _, pr := range prs {
		if pr.Name == "" {
			continue
		}
		fmt.Fprintf(w, "Protocol%s = %d", pr.Name, pr.Value)
		s := pr.Descr
		if s == "" {
			s = pr.OrigName
		}
		fmt.Fprintf(w, "// %s\n", s)
	}
	fmt.Fprintf(w, ")\n")
	return nil
}

type protocolNumbers struct {
	XMLName  xml.Name `xml:"registry"`
	Title    string   `xml:"title"`
	Updated  string   `xml:"updated"`
	RegTitle string   `xml:"registry>title"`
	Note     string   `xml:"registry>note"`
	Records  []struct {
		Value string `xml:"value"`
		Name  string `xml:"name"`
		Descr string `xml:"description"`
	} `xml:"registry>record"`
}

type canonProtocolRecord struct {
	OrigName string
	Name     string
	Descr    string
	Value    int
}

func (pn *protocolNumbers) escape() []canonProtocolRecord {
	prs := make([]canonProtocolRecord, len(pn.Records))
	sr := strings.NewReplacer(
		"-in-", "in",
		"-within-", "within",
		"-over-", "over",
		"+", "P",
		"-", "",
		"/", "",
		".", "",
		" ", "",
	)
	for i, pr := range pn.Records {
		if strings.Contains(pr.Name, "Deprecated") ||
			strings.Contains(pr.Name, "deprecated") {
			continue
		}
		prs[i].OrigName = pr.Name
		s := strings.TrimSpace(pr.Name)
		switch pr.Name {
		case "ISIS over IPv4":
			prs[i].Name = "ISIS"
		case "manet":
			prs[i].Name = "MANET"
		default:
			prs[i].Name = sr.Replace(s)
		}
		ss := strings.Split(pr.Descr, "\n")
		for i := range ss {
			ss[i] = strings.TrimSpace(ss[i])
		}
		if len(ss) > 1 {
			prs[i].Descr = strings.Join(ss, " ")
		} else {
			prs[i].Descr = ss[0]
		}
		prs[i].Value, _ = strconv.Atoi(pr.Value)
	}
	return prs
}

func parseAddrFamilyNumbers(w io.Writer, r io.Reader) error {
	dec := xml.NewDecoder(r)
	var afn addrFamilylNumbers
	if err := dec.Decode(&afn); err != nil {
		return err
	}
	afrs := afn.escape()
	fmt.Fprintf(w, "// %s, Updated: %s\n", afn.Title, afn.Updated)
	fmt.Fprintf(w, "const (\n")
	for _, afr := range afrs {
		if afr.Name == "" {
			continue
		}
		fmt.Fprintf(w, "AddrFamily%s = %d", afr.Name, afr.Value)
		fmt.Fprintf(w, "// %s\n", afr.Descr)
	}
	fmt.Fprintf(w, ")\n")
	return nil
}

type addrFamilylNumbers struct {
	XMLName  xml.Name `xml:"registry"`
	Title    string   `xml:"title"`
	Updated  string   `xml:"updated"`
	RegTitle string   `xml:"registry>title"`
	Note     string   `xml:"registry>note"`
	Records  []struct {
		Value string `xml:"value"`
		Descr string `xml:"description"`
	} `xml:"registry>record"`
}

type canonAddrFamilyRecord struct {
	Name  string
	Descr string
	Value int
}

func (afn *addrFamilylNumbers) escape() []canonAddrFamilyRecord {
	afrs := make([]canonAddrFamilyRecord, len(afn.Records))
	sr := strings.NewReplacer(
		"IP version 4", "IPv4",
		"IP version 6", "IPv6",
		"Identifier", "ID",
		"-", "",
		"-", "",
		"/", "",
		".", "",
		" ", "",
	)
	for i, afr := range afn.Records {
		if strings.Contains(afr.Descr, "Unassigned") ||
			strings.Contains(afr.Descr, "Reserved") {
			continue
		}
		afrs[i].Descr = afr.Descr
		s := strings.TrimSpace(afr.Descr)
		switch s {
		case "IP (IP version 4)":
			afrs[i].Name = "IPv4"
		case "IP6 (IP version 6)":
			afrs[i].Name = "IPv6"
		case "AFI for L2VPN information":
			afrs[i].Name = "L2VPN"
		case "E.164 with NSAP format subaddress":
			afrs[i].Name = "E164withSubaddress"
		case "MT IP: Multi-Topology IP version 4":
			afrs[i].Name = "MTIPv4"
		case "MAC/24":
			afrs[i].Name = "MACFinal24bits"
		case "MAC/40":
			afrs[i].Name = "MACFinal40bits"
		case "IPv6/64":
			afrs[i].Name = "IPv6Initial64bits"
		default:
			n := strings.Index(s, "(")
			if n > 0 {
				s = s[:n]
			}
			n = strings.Index(s, ":")
			if n > 0 {
				s = s[:n]
			}
			afrs[i].Name = sr.Replace(s)
		}
		afrs[i].Value, _ = strconv.Atoi(afr.Value)
	}
	return afrs
}
