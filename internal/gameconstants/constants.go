package gameconstants

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
)

type xmlConstants struct {
	Enums []xmlEnum `xml:"enum"`
}

type xmlEnum struct {
	Name   string     `xml:"name,attr"`
	Type   string     `xml:"type,attr"`
	Consts []xmlConst `xml:"const"`
	Enums  []xmlEnum  `xml:"enum"`
	Ranges []xmlRange `xml:"range"`
}

type xmlConst struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

type xmlRange struct {
	Name   string `xml:"name,attr"`
	Values string `xml:"values,attr"`
}

type enumData struct {
	Type   string              `json:"type,omitempty"`
	Values map[string]any      `json:"values"`
	Ranges map[string][]string `json:"ranges,omitempty"`
}

type constantsFile struct {
	Enums map[string]enumData `json:"enums"`
}

type constantsDocument struct {
	Version struct {
		Version string `json:"VERSION,omitempty"`
		Build   uint64 `json:"BUILD"`
	} `json:"VERSION"`
	XMLConstants map[string]constantsFile `json:"XML_CONSTANTS"`
}

// Generate converts unpacked gui/data/constants XML files into a versioned JSON subset.
func Generate(files map[string][]byte, build string) (data []byte, err error) {
	data, err = GenerateWithScripts(files, nil, build)
	return
}

// GenerateWithScripts adds replay tables recovered from encrypted Python modules.
func GenerateWithScripts(files map[string][]byte, scripts map[string][]byte, build string) (data []byte, err error) {
	var (
		document           constantsDocument
		fileNames          []string
		xmlData            xmlConstants
		filePath, baseName string
		decoded            constantsFile
		buildNumber        uint64
		unitTypes          []string
		version            map[string]any
		clientVersion      string
		clientPatch        float64
		patchAvailable     bool
		replayTables       map[string]any
		sharedConstants    map[string]any
	)

	if len(files) == 0 {
		err = fmt.Errorf("no gui/data/constants XML files found")
		return
	}

	if buildNumber, err = strconv.ParseUint(build, 10, 64); err != nil {
		err = fmt.Errorf("invalid build number %q: %w", build, err)
		return
	}

	document.Version.Build = buildNumber
	replayTables = make(map[string]any)
	document.XMLConstants = make(map[string]constantsFile, len(files))
	for filePath = range files {
		fileNames = append(fileNames, filePath)
	}

	sort.Strings(fileNames)

	for _, filePath = range fileNames {
		xmlData = xmlConstants{}
		if err = xml.Unmarshal(files[filePath], &xmlData); err != nil {
			err = fmt.Errorf("parse %s: %w", filePath, err)
			return
		}

		decoded = constantsFile{Enums: make(map[string]enumData)}
		for _, enum := range xmlData.Enums {
			if err = addEnum(decoded.Enums, "", enum); err != nil {
				err = fmt.Errorf("parse %s: %w", filePath, err)
				return
			}
		}

		baseName = strings.TrimSuffix(path.Base(filePath), path.Ext(filePath))
		document.XMLConstants[baseName] = decoded
	}

	if len(scripts) > 0 {
		if unitTypes, err = decodeUnitTypes(scripts["scripts/m140717bd.pyc"]); err != nil {
			err = fmt.Errorf("decode UNIT_TYPES: %w", err)
			return
		}

		if clientVersion, err = decodeClientVersion(scripts["scripts/Version.pyc"]); err != nil {
			err = fmt.Errorf("decode VERSION.VERSION: %w", err)
			return
		}

		if clientPatch, patchAvailable, err = decodeClientPatch(scripts["scripts/Version.pyc"]); err != nil {
			err = fmt.Errorf("decode VERSION.PATCH: %w", err)
			return
		}

		if replayTables, err = decodeReplayTables(scripts); err != nil {
			err = fmt.Errorf("decode replay constants: %w", err)
			return
		}
	}

	version = map[string]any{"BUILD": document.Version.Build}
	if clientVersion != "" {
		version["VERSION"] = clientVersion
	}

	if patchAvailable {
		version["PATCH"] = clientPatch
	}

	if sharedConstants, err = decodeSharedDataConstants(scripts["scripts/SharedDataConstants.pyc"]); err != nil {
		err = fmt.Errorf("decode SHARED_DATA_CONSTANTS: %w", err)
		return
	} else if len(sharedConstants) > 0 {
		replayTables["SHARED_DATA_CONSTANTS"] = sharedConstants
	}

	if gameModes, ok := decodeGameModes(document.XMLConstants); ok {
		replayTables["GAME_MODES"] = gameModes
	}

	if shipTypes, ok := decodeShipTypes(files); ok {
		replayTables["SHIP_TYPES"] = shipTypes
	}

	if deathReasons, ok, decodeErr := decodeDeathReasons(files["gui/data/constants/battle.xml"], scripts["scripts/DeathReason.pyc"]); decodeErr != nil {
		err = fmt.Errorf("decode DEATH_REASONS: %w", decodeErr)
		return
	} else if ok {
		replayTables["DEATH_REASONS"] = deathReasons
	}

	if len(scripts["scripts/Avatar.pyc"]) > 0 {
		replayTables["DAMAGE_STATS"] = map[string]any{}
	}

	if data, err = marshalDocument(document, version, unitTypes, replayTables); err != nil {
		return
	}

	return
}

func marshalDocument(document constantsDocument, version map[string]any, unitTypes []string, replayTables map[string]any) (data []byte, err error) {
	var output map[string]any
	output = map[string]any{"VERSION": version, "XML_CONSTANTS": document.XMLConstants}
	if len(unitTypes) > 0 {
		output["UNIT_TYPES"] = unitTypes
	}

	for name, value := range replayTables {
		output[name] = value
	}

	if data, err = json.MarshalIndent(output, "", "  "); err != nil {
		return
	}

	data = append(data, '\n')
	return
}

// addEnum records constants and named ranges, including nested XML enums.
func addEnum(enums map[string]enumData, parent string, source xmlEnum) (err error) {
	var (
		name         string
		entry        enumData
		value        any
		rangeValues  []string
		constantName string
		nextInteger  int64
	)

	if source.Name == "" {
		err = fmt.Errorf("enum has no name")
		return
	}

	name = source.Name
	if parent != "" {
		name = parent + "." + name
	}

	if _, exists := enums[name]; exists {
		err = fmt.Errorf("duplicate enum %q", name)
		return
	}

	entry = enumData{Type: source.Type, Values: make(map[string]any)}
	for _, constant := range source.Consts {
		if constant.Name == "" {
			err = fmt.Errorf("enum %q has a constant without a name", name)
			return
		}

		constantName = constant.Name
		if constant.Value == "" && strings.EqualFold(source.Type, "Integer") {
			value = nextInteger
		} else if value, err = parseValue(constant.Value); err != nil {
			err = fmt.Errorf("enum %q constant %q: %w", name, constantName, err)
			return
		}

		entry.Values[constantName] = value
		if integerValue, ok := value.(int64); ok {
			nextInteger = integerValue + 1
		}
	}

	for _, enum := range source.Enums {
		if err = addEnum(enums, name, enum); err != nil {
			return
		}
	}

	if len(source.Ranges) > 0 {
		entry.Ranges = make(map[string][]string, len(source.Ranges))
	}

	for _, item := range source.Ranges {
		if item.Name == "" {
			err = fmt.Errorf("enum %q has a range without a name", name)
			return
		}

		if item.Values != "" {
			for _, rangeValue := range strings.Split(item.Values, ",") {
				rangeValues = append(rangeValues, strings.TrimSpace(rangeValue))
			}
		} else {
			rangeValues = []string{}
		}

		entry.Ranges[item.Name] = rangeValues
		rangeValues = nil
	}

	enums[name] = entry
	return
}

// parseValue preserves enum values as JSON numbers when they are numeric.
func parseValue(raw string) (value any, err error) {
	var (
		integer int64
		decimal float64
	)

	if integer, err = strconv.ParseInt(raw, 10, 64); err == nil {
		value = integer
		err = nil
		return
	}

	if integer, err = strconv.ParseInt(raw, 0, 64); err == nil {
		value = integer
		err = nil
		return
	}

	if decimal, err = strconv.ParseFloat(raw, 64); err == nil {
		value = decimal
		err = nil
		return
	}

	value = raw
	err = nil
	return
}
