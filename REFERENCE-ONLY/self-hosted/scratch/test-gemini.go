package main

import (
	"context"
	"fmt"
	"os"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

func main() {
	ctx := context.Background()
	client, err := genai.NewClient(ctx, option.WithAPIKey(os.Getenv("GEMINI_API_KEY")))
	if err != nil {
		fmt.Println("client err:", err)
		return
	}
	model := client.GenerativeModel("gemini-1.5-flash") // using correct model to see if the issue is just the model name
	resp, err := model.GenerateContent(ctx, genai.Text("hello"))
	if err != nil {
		fmt.Println("generate err:", err)
		return
	}
	fmt.Println("success!", len(resp.Candidates))
}
