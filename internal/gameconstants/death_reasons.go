package gameconstants

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

var (
	deathReasonComment = regexp.MustCompile(`DeathReason\(next\(enum\),\s*(?:(?:'([^']+)'|DEATH_REASON_NAME\.([A-Z0-9_]+))\s*,?\s*)?([^)]*)\)+\s*#\s*(\d+)`)
	deathReasonIcon    = regexp.MustCompile(`\bicon='([^']+)'`)
	deathReasonSound   = regexp.MustCompile(`\bsound='([^']+)'`)
)

// decodeDeathReasons reads names and metadata from enum comments and client defaults from bytecode.
func decodeDeathReasons(xmlData, pycData []byte) (reasons map[string]any, valid bool, err error) {
	var (
		code          *marshalCode
		object        *marshalCode
		stringsIn     []string
		moduleStrings []string
		decoder       *xml.Decoder
		parsedXML     xmlConstants
		enumNames     []string
		enum          xmlEnum
		token         xml.Token
		comment       xml.Comment
		match         []string
		iconMatch     []string
		soundMatch    []string
		name          string
		icon          string
		sound         string
		id            int
		defaultsFound bool
	)

	reasons = make(map[string]any)
	if len(xmlData) == 0 || len(pycData) == 0 {
		return
	}

	if err = xml.Unmarshal(xmlData, &parsedXML); err != nil {
		return
	}

	for _, enum = range parsedXML.Enums {
		if enum.Name == "DEATH_REASON_NAME" {
			enumNames = make([]string, len(enum.Consts))
			for index, constant := range enum.Consts {
				enumNames[index] = constant.Value
			}

			break
		}
	}

	if len(enumNames) == 0 {
		return
	}

	if code, err = DecodePYC(pycData); err != nil {
		return
	}

	moduleStrings = StringConstants(code)
	for _, object = range CodeObjects(code) {
		stringsIn = StringConstants(object)
		if containsString(stringsIn, "frags") && containsString(stringsIn, "Health") {
			defaultsFound = true
			break
		}
	}

	if !defaultsFound {
		return
	}

	decoder = xml.NewDecoder(bytes.NewReader(xmlData))
	for {
		if token, err = decoder.Token(); err != nil {
			if err == io.EOF {
				err = nil
				break
			}

			return
		}

		if comment, valid = token.(xml.Comment); !valid {
			continue
		}

		match = deathReasonComment.FindStringSubmatch(string(comment))
		if len(match) != 5 {
			continue
		}

		name = match[1]
		if name == "" {
			name = match[2]
		}

		if id, err = strconv.Atoi(match[4]); err != nil {
			err = fmt.Errorf("invalid death reason ID %q: %w", match[4], err)
			return
		}

		if name == "" && id >= 0 && id < len(enumNames) {
			name = enumNames[id]
		}

		iconMatch = deathReasonIcon.FindStringSubmatch(match[3])
		soundMatch = deathReasonSound.FindStringSubmatch(match[3])
		icon = "frags"

		if len(iconMatch) == 2 {
			icon = iconMatch[1]
		} else if name == "MISSILE" && containsString(moduleStrings, "icon_frag_missile") {
			icon = "icon_frag_missile"
		}

		sound = "Health"
		if len(soundMatch) == 2 {
			sound = soundMatch[1]
		}

		reasons[strconv.Itoa(id)] = map[string]any{"sound": sound, "icon": icon, "id": id, "name": name}
	}

	valid = len(reasons) > 0
	return
}

func containsString(values []string, expected string) (found bool) {
	for _, value := range values {
		if strings.TrimSpace(value) == expected {
			found = true
			return
		}
	}
	return
}
