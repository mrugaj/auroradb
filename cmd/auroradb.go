package main

import (
	"auroradb/db"
	"auroradb/engine"
	"auroradb/types"
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/olekukonko/tablewriter"
)

func main() {
	var database *db.DB
	var closeFunc func()

	fmt.Println("Starting AuroraDB Interactive CLI...")
	fmt.Println("Example: open mydb.db;")

	for {
		input := getQueryInput()
		if input == "" {
			continue
		}

		inputArr := strings.Split(input, " ")
		switch inputArr[0] {
		case "open":
			if len(inputArr) < 2 {
				fmt.Println("Usage: open <filename>;")
				continue
			}
			name := strings.Trim(inputArr[1], ";")
			fmt.Println("Opening database:", name)
			database, closeFunc = db.OpenDB(name)
			fmt.Println("Successfully connected.")
		case "exit;":
			fmt.Println("Exiting AuroraDB...")
			if closeFunc != nil {
				closeFunc()
			}
			os.Exit(0)
		default:
			if database == nil {
				fmt.Println("Please run 'open <filename>;' first.")
				continue
			}
			execQuery(input, database)
		}
	}
}

func execQuery(query string, database *db.DB) {
	res, err := engine.ExecuteQuery(query, database)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	if res == nil {
		fmt.Println("Success.")
		return
	}

	switch val := res.(type) {
	case []types.Record:
		printRecords(val)
	case int:
		fmt.Printf("Affected %d record(s)\n", val)
	}
}

func printRecords(recs []types.Record) {
	if len(recs) == 0 {
		fmt.Println("No records found.")
		return
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader(recs[0].Cols)

	for _, r := range recs {
		table.Append(r.ToString())
	}

	table.Render()
}

func getQueryInput() string {
	res := ""
	for {
		fmt.Print("aurora>> ")
		reader := bufio.NewReader(os.Stdin)
		line, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("Read error:", err)
			return ""
		}
		line = strings.TrimSpace(line)
		res += " " + line
		if strings.HasSuffix(line, ";") {
			break
		}
	}
	return strings.TrimSpace(res)
}
