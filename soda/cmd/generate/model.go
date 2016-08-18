package generate

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/markbates/going/defaults"
	"github.com/markbates/inflect"
	"github.com/markbates/pop"
	"github.com/spf13/cobra"
)

var skipMigration bool

func init() {
	ModelCmd.Flags().BoolVarP(&skipMigration, "skip-migration", "s", false, "Skip creating a new fizz migration for this model.")
}

var nrx = regexp.MustCompile(`^nulls.(.+)`)

type names struct {
	Original string
	Table    string
	Proper   string
	File     string
	Plural   string
}

func newName(name string) names {
	return names{
		Original: name,
		File:     name,
		Table:    inflect.Tableize(name),
		Proper:   inflect.Camelize(name),
		Plural:   inflect.Pluralize(inflect.Camelize(name)),
	}
}

type attribute struct {
	Names        names
	OriginalType string
	GoType       string
	Nullable     bool
}

func (a attribute) String() string {
	return fmt.Sprintf("\t%s %s `json:\"%s\" db:\"%s\"`", a.Names.Proper, a.GoType, a.Names.Original, a.Names.Original)
}

type model struct {
	Package    string
	Imports    []string
	Names      names
	Attributes []attribute
}

func (m model) String() string {
	s := []string{fmt.Sprintf("package %s\n", m.Package)}
	if len(m.Imports) == 1 {
		s = append(s, fmt.Sprintf("import \"%s\"\n", m.Imports[0]))
	} else {
		s = append(s, "import (")
		for _, im := range m.Imports {
			s = append(s, fmt.Sprintf("\t\"%s\"", im))
		}
		s = append(s, ")\n")
	}

	s = append(s, fmt.Sprintf("// %s maps to the database table '%s'", m.Names.Proper, m.Names.Table))
	s = append(s, fmt.Sprintf("type %s struct {", m.Names.Proper))
	for _, a := range m.Attributes {
		s = append(s, a.String())
	}
	s = append(s, "}")
	s = append(s, fmt.Sprintf("\ntype %s []%s", m.Names.Plural, m.Names.Proper))

	return strings.Join(s, "\n")
}

func (m model) Fizz() string {
	s := []string{fmt.Sprintf("create_table(\"%s\", func(t) {", m.Names.Table)}
	for _, a := range m.Attributes {
		switch a.Names.Original {
		case "id", "created_at", "updated_at":
		default:
			x := fmt.Sprintf("\tt.Column(\"%s\", \"%s\", {})", a.Names.Original, fizzColType(a.OriginalType))
			if a.Nullable {
				x = strings.Replace(x, "{}", `{"null": true}`, -1)
			}
			s = append(s, x)
		}
	}
	s = append(s, "})")
	return strings.Join(s, "\n")
}

func newModel(name string) model {
	id := newName("id")
	id.Proper = "ID"
	return model{
		Package: "models",
		Imports: []string{"time"},
		Names:   newName(name),
		Attributes: []attribute{
			{Names: id, OriginalType: "int", GoType: "int"},
			{Names: newName("created_at"), OriginalType: "time.Time", GoType: "time.Time"},
			{Names: newName("updated_at"), OriginalType: "time.Time", GoType: "time.Time"},
		},
	}
}

var ModelCmd = &cobra.Command{
	Use:     "model [name]",
	Aliases: []string{"m"},
	Short:   "Generates a model for your database",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return errors.New("You must supply a name for your model!")
		}

		model := newModel(args[0])

		hasNulls := false
		for _, def := range args[1:] {
			col := strings.Split(def, ":")
			if len(col) == 1 {
				col = append(col, "string")
			}
			nullable := nrx.MatchString(col[1])
			if !hasNulls && nullable {
				hasNulls = true
				model.Imports = append(model.Imports, "github.com/markbates/going/nulls")
			}
			model.Attributes = append(model.Attributes, attribute{
				Names:        newName(col[0]),
				OriginalType: col[1],
				GoType:       colType(col[1]),
				Nullable:     nullable,
			})
		}

		err := os.MkdirAll("models", 0766)
		if err != nil {
			return err
		}

		fname := filepath.Join("models", model.Names.File+".go")
		err = ioutil.WriteFile(fname, []byte(model.String()), 0666)
		if err != nil {
			return err
		}

		err = ioutil.WriteFile(filepath.Join("models", model.Names.File+"_test.go"), []byte(`package models_test`), 0666)
		if err != nil {
			return err
		}

		md, _ := filepath.Abs(fname)
		goi := exec.Command("gofmt", "-w", md)
		out, err := goi.CombinedOutput()
		if err != nil {
			fmt.Printf("Received an error when trying to run gofmt -> %#v\n", err)
			fmt.Println(out)
		}

		b, err := ioutil.ReadFile(fname)
		if err != nil {
			return err
		}

		fmt.Println(string(b))

		if !skipMigration {
			cflag := cmd.Flag("path")
			migrationPath := defaults.String(cflag.Value.String(), "./migrations")
			err = pop.MigrationCreate(migrationPath, fmt.Sprintf("create_%s", model.Names.Table), "fizz", []byte(model.Fizz()), []byte(fmt.Sprintf("drop_table(\"%s\")", model.Names.Table)))
			if err != nil {
				return err
			}
		}

		return nil
	},
}

func colType(s string) string {
	switch s {
	case "text":
		return "string"
	case "time", "timestamp":
		return "time.Time"
	default:
		return s
	}
	return s
}

func fizzColType(s string) string {
	if nrx.MatchString(s) {
		return fizzColType(strings.Replace(s, "nulls.", "", -1))
	}
	switch strings.ToLower(s) {
	case "int":
		return "integer"
	case "time":
		return "timestamp"
	default:
		return strings.ToLower(s)
	}
	return s
}
