package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
)

func main() {
	dsn := os.Getenv("QA_DSN")
	sql := strings.Join(os.Args[1:], " ")
	conn, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		fmt.Println("connect error:", err)
		os.Exit(1)
	}
	defer conn.Close(context.Background())
	rows, err := conn.Query(context.Background(), sql)
	if err != nil {
		fmt.Println("query error:", err)
		os.Exit(1)
	}
	defer rows.Close()
	fields := rows.FieldDescriptions()
	names := make([]string, len(fields))
	for i, f := range fields {
		names[i] = string(f.Name)
	}
	fmt.Println(strings.Join(names, " | "))
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			fmt.Println("row error:", err)
			continue
		}
		strs := make([]string, len(vals))
		for i, v := range vals {
			strs[i] = fmt.Sprintf("%v", v)
		}
		fmt.Println(strings.Join(strs, " | "))
	}
	if err := rows.Err(); err != nil {
		fmt.Println("rows error:", err)
	}
}
