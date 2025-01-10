package snmp

import (
	"errors"
	"fmt"
	"strings"

	"github.com/sleepinggenius2/gosmi"
	"github.com/sleepinggenius2/gosmi/models"
	"github.com/sleepinggenius2/gosmi/types"

	"github.com/influxdata/telegraf"
)

var errCannotFormatUnkownType = errors.New("cannot format value, unknown type")

type gosmiTranslator struct {
}

func NewGosmiTranslator(paths []string, log telegraf.Logger) (*gosmiTranslator, error) {
	err := LoadMibsFromPath(paths, log, &GosmiMibLoader{})
	if err == nil {
		return &gosmiTranslator{}, nil
	}
	return nil, err
}

//nolint:revive //function-result-limit conditionally 5 return results allowed
func (g *gosmiTranslator) SnmpTranslate(oid string) (mibName string, oidNum string, oidText string, conversion string, err error) {
	mibName, oidNum, oidText, conversion, _, err = snmpTranslateCall(oid)
	return mibName, oidNum, oidText, conversion, err
}

// snmpTable resolves the given OID as a table, providing information about the
// table and fields within.
//
//nolint:revive //Too many return variable but necessary
func (g *gosmiTranslator) SnmpTable(oid string) (
	mibName string, oidNum string, oidText string,
	fields []Field,
	err error) {
	mibName, oidNum, oidText, _, node, err := snmpTranslateCall(oid)
	if err != nil {
		return "", "", "", nil, fmt.Errorf("translating: %w", err)
	}

	mibPrefix := mibName + "::"

	col, tagOids := getIndex(mibPrefix, node)
	for _, c := range col {
		_, isTag := tagOids[mibPrefix+c]
		fields = append(fields, Field{Name: c, Oid: mibPrefix + c, IsTag: isTag})
	}

	return mibName, oidNum, oidText, fields, nil
}

func (g *gosmiTranslator) SnmpFormatEnum(oid string, value interface{}, full bool) (string, error) {
	if value == nil {
		return "", nil
	}

	//nolint:dogsled // only need to get the node
	_, _, _, _, node, err := snmpTranslateCall(oid)
	if err != nil {
		return "", err
	}

	if node.Type == nil {
		return "", errCannotFormatUnkownType
	}

	var v models.Value
	if full {
		v = node.FormatValue(value, models.FormatEnumName, models.FormatEnumValue)
	} else {
		v = node.FormatValue(value, models.FormatEnumName)
	}

	return v.String(), nil
}

func (g *gosmiTranslator) SnmpFormatDisplayHint(oid string, value interface{}) (string, error) {
	if value == nil {
		return "", nil
	}

	//nolint:dogsled // only need to get the node
	_, _, _, _, node, err := snmpTranslateCall(oid)
	if err != nil {
		return "", err
	}

	if node.Type == nil {
		return "", errCannotFormatUnkownType
	}

	return node.FormatValue(value).String(), nil
}

func getIndex(mibPrefix string, node gosmi.SmiNode) (col []string, tagOids map[string]struct{}) {
	// first attempt to get the table's tags
	// mimcks grabbing INDEX {} that is returned from snmptranslate -Td MibName
	indices := node.GetIndex()
	tagOids = make(map[string]struct{}, len(indices))
	for _, index := range indices {
		tagOids[mibPrefix+index.Name] = struct{}{}
	}

	// grabs all columns from the table
	// mimmicks grabbing everything returned from snmptable -Ch -Cl -c public 127.0.0.1 oidFullName
	_, col = node.GetColumns()

	return col, tagOids
}

//nolint:revive //Too many return variable but necessary
func snmpTranslateCall(oid string) (mibName string, oidNum string, oidText string, conversion string, node gosmi.SmiNode, err error) {
	var out gosmi.SmiNode
	var end string
	if strings.ContainsAny(oid, "::") {
		// split given oid
		// for example RFC1213-MIB::sysUpTime.0
		s := strings.SplitN(oid, "::", 2)
		// moduleName becomes RFC1213
		moduleName := s[0]
		module, err := gosmi.GetModule(moduleName)
		if err != nil {
			return oid, oid, oid, "", gosmi.SmiNode{}, err
		}
		if s[1] == "" {
			return "", oid, oid, "", gosmi.SmiNode{}, fmt.Errorf("cannot parse %v", oid)
		}
		// node becomes sysUpTime.0
		node := s[1]
		if strings.ContainsAny(node, ".") {
			s = strings.SplitN(node, ".", 2)
			// node becomes sysUpTime
			node = s[0]
			end = "." + s[1]
		}

		out, err = module.GetNode(node)
		if err != nil {
			return oid, oid, oid, "", out, err
		}

		if oidNum = out.RenderNumeric(); oidNum == "" {
			return oid, oid, oid, "", out, fmt.Errorf("cannot translate %v into a numeric OID, please ensure all imported MIBs are in the path", oid)
		}

		oidNum = "." + oidNum + end
	} else if strings.ContainsAny(oid, "abcdefghijklnmopqrstuvwxyz") {
		//handle mixed oid ex. .iso.2.3
		s := strings.Split(oid, ".")
		for i := range s {
			if strings.ContainsAny(s[i], "abcdefghijklmnopqrstuvwxyz") {
				out, err = gosmi.GetNode(s[i])
				if err != nil {
					return oid, oid, oid, "", out, err
				}
				s[i] = out.RenderNumeric()
			}
		}
		oidNum = strings.Join(s, ".")
		out, err = gosmi.GetNodeByOID(types.OidMustFromString(oidNum))
		if err != nil {
			return oid, oid, oid, "", out, err
		}
	} else {
		out, err = gosmi.GetNodeByOID(types.OidMustFromString(oid))
		oidNum = oid
		// ensure modules are loaded or node will be empty (might not error)
		//nolint:nilerr // do not return the err as the oid is numeric and telegraf can continue
		if err != nil || out.Name == "iso" {
			return oid, oid, oid, "", out, nil
		}
	}

	tc := out.GetSubtree()
	for i := range tc {
		// case where the mib doesn't have a conversion so Type struct will be nil
		// prevents seg fault
		if tc[i].Type == nil {
			break
		}

		if tc[i].Type.Format != "" {
			conversion = "displayhint"
		} else {
			switch tc[i].Type.Name {
			case "InetAddress", "IPSIpAddress":
				conversion = "ipaddr"
			}
		}
	}

	oidText = out.RenderQualified()
	i := strings.Index(oidText, "::")
	if i == -1 {
		return "", oid, oid, "", out, errors.New("not found")
	}
	mibName = oidText[:i]
	oidText = oidText[i+2:] + end

	return mibName, oidNum, oidText, conversion, out, nil
}

// The following is for snmp_trap
type MibEntry struct {
	MibName string
	OidText string
}

func TrapLookup(oid string) (e MibEntry, err error) {
	var givenOid types.Oid
	if givenOid, err = types.OidFromString(oid); err != nil {
		return e, fmt.Errorf("could not convert OID %s: %w", oid, err)
	}

	// Get node name
	var node gosmi.SmiNode
	if node, err = gosmi.GetNodeByOID(givenOid); err != nil {
		return e, err
	}
	e.OidText = node.Name

	// Add not found OID part
	if !givenOid.Equals(node.Oid) {
		e.OidText += "." + givenOid[len(node.Oid):].String()
	}

	// Get module name
	module := node.GetModule()
	if module.Name != "<well-known>" {
		e.MibName = module.Name
	}

	return e, nil
}
