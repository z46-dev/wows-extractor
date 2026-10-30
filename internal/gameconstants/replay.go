package gameconstants

import (
	"encoding/xml"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// decodeShipTypes uses the client's declared ship type order as numeric IDs.
func decodeShipTypes(files map[string][]byte) (shipTypes map[string]int, valid bool) {
	var (
		data     xmlConstants
		enum     xmlEnum
		shipType string
		err      error
	)

	if err = xml.Unmarshal(files["gui/data/constants/common.xml"], &data); err != nil {
		return
	}

	for _, enum = range data.Enums {
		if enum.Name != "SHIP_TYPE" {
			continue
		}

		shipTypes = make(map[string]int, len(enum.Consts))
		for index, constant := range enum.Consts {
			switch constant.Name {
			case "AIRCARRIER":
				shipType = "AirCarrier"
			case "BATTLESHIP":
				shipType = "Battleship"
			case "CRUISER":
				shipType = "Cruiser"
			case "DESTROYER":
				shipType = "Destroyer"
			case "AUXILIARY":
				shipType = "Auxiliary"
			case "SUBMARINE":
				shipType = "Submarine"
			default:
				return
			}

			if constant.Value != shipType {
				return
			}

			shipTypes[shipType] = index
		}

		valid = len(shipTypes) == 6
		return
	}

	return
}

func decodeClientVersion(data []byte) (version string, err error) {
	var (
		code  *marshalCode
		part  string
		parts []string
		valid bool = true
	)

	if len(data) == 0 {
		return
	}

	if code, err = DecodePYC(data); err != nil {
		return
	}

	for _, version = range StringConstants(code) {
		if parts = strings.Split(version, "."); len(parts) != 2 {
			continue
		}

		for _, part = range parts {
			if _, err = strconv.ParseUint(part, 10, 16); err != nil {
				valid = false
				err = nil
				break
			}
		}

		if valid {
			return
		}

		valid = true
	}

	version = ""
	return
}

func decodeClientPatch(data []byte) (patch float64, available bool, err error) {
	var (
		code    *marshalCode
		value   any
		found   bool
		current float64
	)

	if len(data) == 0 {
		return
	}

	if code, err = DecodePYC(data); err != nil {
		return
	}

	for _, object := range CodeObjects(code) {
		for _, value = range object.Consts {
			if current, found = value.(float64); found {
				if available && patch != current {
					err = fmt.Errorf("conflicting patch values %v and %v", patch, current)
					return
				}

				patch = current
				available = true
			}
		}
	}

	return
}

func decodeUnitTypes(data []byte) (values []string, err error) {
	var code *marshalCode

	if len(data) == 0 {
		return
	}

	if code, err = DecodePYC(data); err != nil {
		return
	}

	for _, value := range StringConstants(code) {
		if value != "Unit" && strings.HasSuffix(value, "Unit") && !slices.Contains(values, value) {
			values = append(values, value)
		}
	}

	return
}

func decodeSharedDataConstants(data []byte) (values map[string]any, err error) {
	var (
		code        *marshalCode
		assignments map[string]any
		list        []string
		name        string
		value       any
		valid       bool
	)

	values = make(map[string]any)
	if len(data) == 0 {
		return
	}

	if code, err = DecodePYC(data); err != nil {
		return
	}

	assignments = EvaluatedAssignments(code)
	for name, value = range assignments {
		if !validConstantName(name) {
			continue
		}

		switch typed := value.(type) {
		case string:
			values[name] = value
		case []any:
			if list, valid = stringList(typed); valid {
				sort.Strings(list)
				values[name] = list
			}
		}
	}

	values["PRE_BATTLE_SIGN"] = "preBattleSign"
	values["INVITATIONS_ENABLES"] = "invitationsEnabled"
	if values["VEHICLE_DATA"], err = sharedDataFields(values, []string{"ANTI_ABUSE_ENABLED", "CAMOUFLAGE_INFO", "CLAN_COLOR", "CLAN_ID", "CLAN_TAG", "CREW_PARAMS", "FRAGS_COUNT", "FRIENDLY_FIRE_ENABLED", "IS_ABUSER", "IS_BOT", "IS_HIDDEN", "IS_TEAM_SHOOTER", "KEY_TARGET_MARKERS", "KILLED_BUILDINGS_COUNT", "MAX_HEALTH", "SHIP_COMPONENTS", "SHIP_CONFIG_DUMP", "SHIP_FRAGS", "SHIP_ID", "SHIP_PARAMS_ID", "SKIN_ID", "TTK_STATUS"}); err != nil {
		return
	}

	if values["AVATAR_DATA"], err = sharedDataFields(values, []string{"AVATAR_ID", "INVITATIONS_ENABLES", "IS_CLIENT_LOADED", "IS_CONNECTED", "IS_LEAVER", "IS_PRE_BATTLE_OWNER", "PLAYER_MODE", "PRE_BATTLE_ID_ON_START", "PRE_BATTLE_SIGN", "PRE_BATTLE_ID"}); err != nil {
		return
	}

	return
}

func sharedDataFields(constants map[string]any, names []string) (fields []string, err error) {
	var (
		name  string
		value any
		valid bool
	)

	for _, name = range names {
		if value, valid = constants[name]; !valid {
			err = fmt.Errorf("shared data field %q is missing", name)
			return
		}

		if field, ok := value.(string); ok {
			fields = append(fields, field)
		} else {
			err = fmt.Errorf("shared data field %q is not a string", name)
			return
		}
	}

	sort.Strings(fields)
	return
}

func decodeGameModes(xmlConstants map[string]constantsFile) (modes map[string]string, valid bool) {
	var (
		common constantsFile
		enum   enumData
		name   string
		value  any
		id     int64
	)

	if common, valid = xmlConstants["battle"]; !valid {
		return
	}

	if enum, valid = common.Enums["GAME_MODE"]; !valid {
		return
	}

	modes = make(map[string]string, len(enum.Values))
	for name, value = range enum.Values {
		if id, valid = value.(int64); valid && id >= 0 {
			if mode, ok := gameModeName(name); ok {
				modes[strconv.FormatInt(id, 10)] = mode
			}
		}
	}

	valid = len(modes) > 0
	return
}

func gameModeName(name string) (mode string, valid bool) {
	var (
		parts []string
		part  string
	)

	switch name {
	case "TUTORIAL", "INVALID":
		return
	case "STANDART":
		mode = "Standard"
	case "STANDARD_DOMINATION":
		mode = "StandDom"
	case "SINGLEBASE":
		mode = "SingleBase"
	case "MEGABASE":
		mode = "MegaBase"
	case "PVE":
		mode = "PVE"
	default:
		parts = strings.Split(strings.ToLower(name), "_")
		for _, part = range parts {
			if part != "" {
				mode += strings.ToUpper(part[:1]) + part[1:]
			}
		}
	}

	valid = mode != ""
	return
}

func validConstantName(value string) (valid bool) {
	var (
		character byte
		index     int
	)

	if value == "" || value[0] < 'A' || value[0] > 'Z' {
		return
	}

	for index = 1; index < len(value); index++ {
		character = value[index]
		if !((character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '_') {
			return
		}
	}

	valid = true
	return
}

func decodeReplayTables(scripts map[string][]byte) (tables map[string]any, err error) {
	var (
		code       *marshalCode
		results    map[string]any
		public     []string
		full       []string
		ribbons    []string
		ribbonIDs  map[string]any
		list       []string
		fullExtra  []string
		shipConfig []string
		name       string
		pythonName string
		tupleFound bool
	)

	tables = make(map[string]any)
	if len(scripts["scripts/m865ba929.pyc"]) == 0 {
		return
	}

	if code, err = DecodePYC(scripts["scripts/m865ba929.pyc"]); err != nil {
		return
	}

	results = AssignedConstants(code)
	for name, pythonName = range map[string]string{
		"CLIENT_BUILDING_INTERACTION_DETAILS": "CLIENT_BUILDING_INTERACTION_DETAILS",
		"CLIENT_VEH_INTERACTION_DETAILS":      "CLIENT_VEH_INTERACTION_DETAILS",
		"COMMON_RESULTS":                      "PLAYER_FULL_RESULTS",
		"INIT_ECONOMICS":                      "INIT_ECONOMICS",
		"COMMON_ECONOMICS":                    "COMMON_ECONOMICS",
		"SUBTOTAL_ECONOMICS":                  "SUBTOTAL_ECONOMICS",
		"PLAYER_PRIVATE_RESULTS":              "PLAYER_PRIVATE_RESULTS",
	} {
		if list, exists := stringList(results[pythonName]); exists {
			tables[name] = list
			if name == "INIT_ECONOMICS" || name == "COMMON_ECONOMICS" || name == "SUBTOTAL_ECONOMICS" || name == "PLAYER_PRIVATE_RESULTS" {
				tables[name+"_INDICES"] = indicesFor(list)
			}
		}
	}

	if list, exists := stringList(results["PLAYER_INFO"]); exists {
		public = append(public, list...)
	}

	if list, exists := stringList(results["VEH_COMMON_RESULTS"]); exists {
		public = append(public, list...)
	}

	for _, list = range StringTuples(code) {
		switch {
		case len(list) > 0 && list[0] == "planes_killed_scouts":
			public = append(public, list...)
			tupleFound = true
		case len(list) > 0 && list[0] == "presence_time_sec":
			fullExtra = list
		}
	}

	if list, exists := stringList(results["SHIP_CONFIG"]); exists {
		shipConfig = list
	}

	code = nil
	if len(scripts["scripts/m1966847e.pyc"]) > 0 {
		if code, err = DecodePYC(scripts["scripts/m1966847e.pyc"]); err != nil {
			return
		}

		for _, value := range StringConstants(code) {
			if strings.HasPrefix(value, "RIBBON_") && value != "RIBBON_WAVE" && value != "RIBBON_TORPEDO_PHOTON" && value != "RIBBON_SHIELD" {
				ribbons = append(ribbons, value)
			}
		}
	}

	if tupleFound && len(public) > 0 && len(ribbons) > 35 {
		public = append(public, ribbons...)
		public = append(public, "SHOT_DOWN_MISSILE")
		tables["CLIENT_PUBLIC_RESULTS"] = public
		tables["CLIENT_PUBLIC_RESULTS_INDICES"] = indicesFor(public)
		ribbonIDs = make(map[string]any, 33)
		for index := range 31 {
			ribbonIDs[strconv.Itoa(index)] = ribbons[index]
		}

		ribbonIDs["34"] = ribbons[34]
		ribbonIDs["35"] = ribbons[35]
		tables["RIBBONS"] = ribbonIDs
		full = append(full, public...)
		full = append(full, fullExtra...)
		full = append(full, shipConfig...)
		if len(fullExtra) > 0 && len(shipConfig) > 0 {
			tables["PLAYER_FULL_RESULTS"] = full
		}
	}

	return
}

func stringList(value any) (values []string, valid bool) {
	var (
		index int
		items []any
		item  any
	)

	if items, valid = value.([]any); !valid {
		return
	}

	values = make([]string, len(items))
	for index, item = range items {
		values[index], valid = item.(string)
		if !valid {
			values = nil
			return
		}
	}

	return
}

func indicesFor(values []string) (indices map[string]int) {
	var (
		index int
		value string
	)

	indices = make(map[string]int, len(values))
	for index, value = range values {
		indices[value] = index
	}

	return
}
