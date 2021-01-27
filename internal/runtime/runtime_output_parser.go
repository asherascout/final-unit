package runtime

import (
	"encoding/json"
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"
)

// Output JSON structure for runtime output
type Output struct {
	Type       string  `json:"type"`
	VarName    string  `json:"var_name"`
	MapKeyType string  `json:"map_key_type"`
	Val        string  `json:"val"`
	ArrIdent   string  `json:"arr_ident"`
	Child      *Output `json:"child"`
}

// ParseLine parses a line of output
func (info *Info) ParseLine(jsonString string, mem *[]string) []string {
	data := Output{}
	err := json.Unmarshal([]byte(jsonString), &data)
	if err != nil {
		log.WithError(err).WithField("line", jsonString).Errorf("unable to parse runtime output")
	}
	stmts := AssertStmts(&data, []Replacement{}, TypeCorrections{}, []Stmt{}, mem)
	res := []string{}
	for _, stmt := range stmts {
		res = append(res, info.Printer.PrintStmt(stmt))
	}
	return res
}

// Replacement struct which contains keys and replacement values
type Replacement struct {
	Key string
	Val string
}

// TypeCorrections struct containing information for printing corrections in types
// e.g. custom values X(int(3)) in order to make correct assert statements
type TypeCorrections struct {
	Prefix string
	Suffix string
}

// AssertStmts new assert statement from runtime output
func AssertStmts(data *Output, replacements []Replacement, typeCorrection TypeCorrections, resStmts []Stmt, mem *[]string) []Stmt {
	if data.Child == nil {
		return CreateAssertStmts(data, replacements, typeCorrection, resStmts)
	}

	switch data.Type {
	case "arr":
		// In case we have an arr type we need to replace the key of the loop at runtime with the value of the loop identifier
		replacements = append(replacements, Replacement{
			Key: data.ArrIdent,
			Val: data.Val,
		})
		return AssertStmts(data.Child, replacements, typeCorrection, resStmts, mem)
	case "custom":
		// Ignore custom values for now, since we test on equal values
		return AssertStmts(data.Child, replacements, typeCorrection, resStmts, mem)
	case "map":
		// In case of map, we need to replace the map key in the loop with the value of the map key at runtime in the identifier
		x := data.Val
		if data.MapKeyType == "string" {
			x = fmt.Sprintf("\"%s\"", x)
		}
		replacements = append(replacements, Replacement{
			Key: data.ArrIdent,
			Val: x,
		})
		return AssertStmts(data.Child, replacements, typeCorrection, resStmts, mem)
	case "pointer":
		// In case pointer we check if value nil, if not
		// we need to use the value of the identifier using the start operator
		if data.Val != "nil" {
			// sanity check
			if data.Child == nil {
				log.Warningf("unable to create assert stmts, expected pointer to have child")
				return []Stmt{}
			}
			pointerStmt := fmt.Sprintf("%s := *%s", data.Child.VarName, data.VarName)
			if !Contains(*mem, pointerStmt) {
				*mem = append(*mem, fmt.Sprintf("%s :=", data.Child.VarName))
				resStmts = append(resStmts, &AssignStmt{
					AssignStmtType: AssignStmtTypeDefine,
					LeftHand:       data.Child.VarName,
					RightHand:      "*" + data.VarName,
				})
			} else {
				resStmts = append(resStmts, &AssignStmt{
					AssignStmtType: AssignSTmtTypeAssign,
					LeftHand:       data.Child.VarName,
					RightHand:      "*" + data.VarName,
				})
			}
		}
		// If nil just continue recursion
		return AssertStmts(data.Child, replacements, typeCorrection, resStmts, mem)
	default:
		return AssertStmts(data.Child, replacements, typeCorrection, resStmts, mem)
	}
}

// CreateAssertStmts creates the eventual assert statement based on runtime output and corrections
func CreateAssertStmts(runtimeOutput *Output, replacements []Replacement, typeCorrection TypeCorrections, resStmts []Stmt) []Stmt {
	for i := 0; i < len(replacements); i++ {
		runtimeOutput.VarName = strings.ReplaceAll(runtimeOutput.VarName, replacements[i].Key, replacements[i].Val)
	}
	for j := 0; j < len(resStmts); j++ {
		for i := 0; i < len(replacements); i++ {
			resStmts[j].Replace(replacements[i].Key, replacements[i].Val)
		}
	}
	switch runtimeOutput.Type {
	case "int",
		"float32",
		"float64",
		"byte",
		"rune",
		"uintptr",
		"uint",
		"uint8",
		"uint16",
		"uint32",
		"uint64",
		"int8",
		"int16",
		"int32",
		"int64":
		return append(resStmts, &AssertStmt{
			AssertStmtType: AssertStmtTypeEqualValues,
			Expected:       fmt.Sprintf("%s%s(%s)%s", typeCorrection.Prefix, runtimeOutput.Type, runtimeOutput.Val, typeCorrection.Suffix),
			Value:          runtimeOutput.VarName,
		})
	case "string":
		return append(resStmts, &AssertStmt{
			AssertStmtType: AssertStmtTypeEqualValues,
			Expected:       fmt.Sprintf("%s%s(`%s`)%s", typeCorrection.Prefix, runtimeOutput.Type, runtimeOutput.Val, typeCorrection.Suffix),
			Value:          runtimeOutput.VarName,
		})
	case "bool":
		if runtimeOutput.Val == "true" {
			return append(resStmts, &AssertStmt{
				AssertStmtType: AssertStmtTypeTrue,
				Expected:       runtimeOutput.VarName,
			})
		}
		return append(resStmts, &AssertStmt{
			AssertStmtType: AssertStmtTypeFalse,
			Expected:       runtimeOutput.VarName,
		})
	case "complex64", "complex128":
		return append(resStmts, &AssertStmt{
			AssertStmtType: AssertStmtTypeEqualValues,
			Expected:       fmt.Sprintf("%s%s%s%s", typeCorrection.Prefix, runtimeOutput.Type, runtimeOutput.Val, typeCorrection.Suffix),
			Value:          runtimeOutput.VarName,
		})
	// Only nil pointers will reach this point
	case "pointer":
		return append(resStmts, &AssertStmt{
			AssertStmtType: AssertStmtTypeNil,
			Expected:       runtimeOutput.VarName,
		})
	case "error":
		if runtimeOutput.Val == "nil" {
			return append(resStmts, &AssertStmt{
				AssertStmtType: AssertStmtTypeNoError,
				Expected:       runtimeOutput.VarName,
			})
		}
		return append(resStmts, &AssertStmt{
			AssertStmtType: AssertStmtTypeError,
			Expected:       runtimeOutput.VarName,
		})
	default:
		log.Warningf("unknown type: %s, value: %s", runtimeOutput.Type, runtimeOutput.Val)
		return []Stmt{}
	}
}
