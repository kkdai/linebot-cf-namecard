package helloworld

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"io"
	"log"
	"os"

	firebase "firebase.google.com/go"
	"firebase.google.com/go/db"
	"github.com/GoogleCloudPlatform/functions-framework-go/functions"
	"google.golang.org/api/option"

	"github.com/google/generative-ai-go/genai"
	"github.com/line/line-bot-sdk-go/v8/linebot"
	"github.com/line/line-bot-sdk-go/v8/linebot/messaging_api"
	"github.com/line/line-bot-sdk-go/v8/linebot/webhook"
)

// Define the context
var fireDB FireDB

// LINE BOt sdk
var bot *messaging_api.MessagingApiAPI
var blob *messaging_api.MessagingApiBlobAPI
var channelToken string

// Gemni API key
var geminiKey string

// define firebase db
type FireDB struct {
	*db.Client
}

// Name card prompt
const ImagePrompt = `
這是一張名片，你是一個名片秘書。請將以下資訊整理成 json 給我。
如果看不出來的，幫我填寫 N/A. 只需要 json 就好:  
Name, Title, Address, Email, Phone, Company.   
其中 Phone 的內容格式為 #886-0123-456-789,1234. 沒有分機就忽略 ,1234`

const LogoImageUrl = "https://raw.githubusercontent.com/kkdai/linebot-smart-namecard/main/img/logo.jpeg"

// Person 定義了 JSON 資料的結構體
type Person struct {
	Name    string `json:"name"`
	Title   string `json:"title"`
	Address string `json:"address"`
	Email   string `json:"email"`
	Phone   string `json:"phone"`
	Company string `json:"company"`
}

const DBCardPath = "namecard"

func init() {
	var err error
	// Init firebase related variables
	ctx := context.Background()
	opt := option.WithCredentialsJSON([]byte(os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")))
	config := &firebase.Config{DatabaseURL: os.Getenv("FIREBASE_URL")}
	app, err := firebase.NewApp(ctx, config, opt)
	if err != nil {
		log.Fatalf("error initializing app: %v", err)
	}
	client, err := app.Database(ctx)
	if err != nil {
		log.Fatalf("error initializing database: %v", err)
	}
	fireDB.Client = client

	// Init LINE Bot related variables
	geminiKey = os.Getenv("GOOGLE_GEMINI_API_KEY")
	channelToken = os.Getenv("ChannelAccessToken")
	bot, err = messaging_api.NewMessagingApiAPI(channelToken)
	if err != nil {
		log.Fatal(err)
	}

	blob, err = messaging_api.NewMessagingApiBlobAPI(channelToken)
	if err != nil {
		log.Fatal(err)
	}

	functions.HTTP("HelloHTTP", HelloHTTP)
}

func HelloHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()

	cb, err := webhook.ParseRequest(os.Getenv("ChannelSecret"), r)
	if err != nil {
		if err == linebot.ErrInvalidSignature {
			w.WriteHeader(400)
		} else {
			w.WriteHeader(500)
		}
		return
	}

	// Init the Gemini AI client
	client, err := genai.NewClient(ctx, option.WithAPIKey(geminiKey))
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	for _, event := range cb.Events {
		log.Printf("Got event %v", event)
		switch e := event.(type) {
		case webhook.MessageEvent:
			switch message := e.Message.(type) {

			// Handle only text messages
			case webhook.TextMessageContent:
				req := message.Text

				// 取得用戶 ID
				var uID string
				switch source := e.Source.(type) {
				case webhook.UserSource:
					uID = source.UserId
				case webhook.GroupSource:
					uID = source.UserId
				case webhook.RoomSource:
					uID = source.UserId
				}
				userPath := fmt.Sprintf("%s/%s", DBCardPath, uID)

				// Load all cards from firebase
				var People map[string]Person
				err = fireDB.NewRef(userPath).Get(ctx, &People)
				if err != nil {
					fmt.Println("load memory failed, ", err)
				}

				// Marshall data to JSON
				jsonData, err := json.Marshal(People)
				if err != nil {
					fmt.Println("Error marshalling data to JSON:", err)
				}

				if message.Text == "list" {
					var cards []messaging_api.FlexBubble
					for _, card := range People {
						// Get URL encode for company name and address
						companyEncode := url.QueryEscape(card.Company)
						addressEncode := url.QueryEscape(card.Address)

						card := messaging_api.FlexBubble{
							Size: messaging_api.FlexBubbleSIZE_GIGA,
							Body: &messaging_api.FlexBox{
								Layout:  messaging_api.FlexBoxLAYOUT_HORIZONTAL,
								Spacing: "md",
								Contents: []messaging_api.FlexComponentInterface{
									&messaging_api.FlexImage{
										AspectMode:  "cover",
										AspectRatio: "1:1",
										Flex:        1,
										Size:        "full",
										Url:         LogoImageUrl,
									},
									&messaging_api.FlexBox{
										Flex:   4,
										Layout: messaging_api.FlexBoxLAYOUT_VERTICAL,
										Contents: []messaging_api.FlexComponentInterface{
											&messaging_api.FlexText{
												Align:  "end",
												Size:   "xxl",
												Text:   card.Name,
												Weight: "bold",
											},
											&messaging_api.FlexText{
												Align: "end",
												Size:  "sm",
												Text:  card.Title,
											},
											&messaging_api.FlexText{
												Align:  "end",
												Margin: "xxl",
												Size:   "lg",
												Text:   card.Company,
												Weight: "bold",
												Action: &messaging_api.UriAction{
													Uri: "https://www.google.com/maps/search/?api=1&query=" + companyEncode + "&openExternalBrowser=1",
												},
											},
											&messaging_api.FlexText{
												Align: "end",
												Size:  "sm",
												Text:  card.Address,
											},
											&messaging_api.FlexText{
												Align:  "end",
												Margin: "xxl",
												Text:   card.Phone,
												Action: &messaging_api.UriAction{
													Uri: "tel:" + card.Phone,
												},
											},
											&messaging_api.FlexText{
												Align: "end",
												Text:  card.Email,
												Action: &messaging_api.UriAction{
													Uri: "mailto:" + card.Email,
												},
											},
											&messaging_api.FlexText{
												Align: "end",
												Text:  "更多資訊",
												Action: &messaging_api.UriAction{
													Uri: "https://github.com/kkdai/linebot-smart-namecard",
												},
											},
										},
									},
								},
							},
						}
						cards = append(cards, card)
					}

					contents := &messaging_api.FlexCarousel{
						Contents: cards,
					}

					if _, err := bot.ReplyMessage(
						&messaging_api.ReplyMessageRequest{
							ReplyToken: e.ReplyToken,
							Messages: []messaging_api.MessageInterface{
								&messaging_api.TextMessage{
									Text: "測試訊息",
								},
								&messaging_api.FlexMessage{
									Contents: contents,
									AltText:  "請到手機上查看名片資訊",
								},
							},
						},
					); err != nil {
						fmt.Println(err)
					}
					continue
				}

				// Add Search prompt
				SearchPrompt := fmt.Sprintf("這是所有的名片資料，請根據輸入文字來查詢相關的名片資料 (%s)，例如: 名字, 職稱, 公司名稱。 查詢問句為： %s, 只要回覆我找到的 JSON Data", jsonData, req)

				// Pass the text content to the gemini-1.5-flash-latest model for text generation
				model := client.GenerativeModel("gemini-1.5-flash-latest")
				res, err := model.GenerateContent(ctx, genai.Text(SearchPrompt))
				if err != nil {
					log.Fatal(err)
				}
				var ret string
				for _, cand := range res.Candidates {
					for _, part := range cand.Content.Parts {
						ret = ret + fmt.Sprintf("%v", part)
						log.Println(part)
					}
				}
				var retPeople map[string]Person
				// unmarshall json to People
				err = json.Unmarshal([]byte(ret), &retPeople)
				if err != nil {
					fmt.Println("Unmarshal failed, ", err, "jsonData:", ret)
				}

				var cards []messaging_api.FlexBubble
				for _, card := range retPeople {
					// Get URL encode for company name and address
					companyEncode := url.QueryEscape(card.Company)
					addressEncode := url.QueryEscape(card.Address)

					card := messaging_api.FlexBubble{
						Size: messaging_api.FlexBubbleSIZE_GIGA,
						Body: &messaging_api.FlexBox{
							Layout:  messaging_api.FlexBoxLAYOUT_HORIZONTAL,
							Spacing: "md",
							Contents: []messaging_api.FlexComponentInterface{
								&messaging_api.FlexImage{
									AspectMode:  "cover",
									AspectRatio: "1:1",
									Flex:        1,
									Size:        "full",
									Url:         LogoImageUrl,
								},
								&messaging_api.FlexBox{
									Flex:   4,
									Layout: messaging_api.FlexBoxLAYOUT_VERTICAL,
									Contents: []messaging_api.FlexComponentInterface{
										&messaging_api.FlexText{
											Align:  "end",
											Size:   "xxl",
											Text:   card.Name,
											Weight: "bold",
										},
										&messaging_api.FlexText{
											Align: "end",
											Size:  "sm",
											Text:  card.Title,
										},
										&messaging_api.FlexText{
											Align:  "end",
											Margin: "xxl",
											Size:   "lg",
											Text:   card.Company,
											Weight: "bold",
											Action: &messaging_api.UriAction{
												Uri: "https://www.google.com/maps/search/?api=1&query=" + companyEncode + "&openExternalBrowser=1",
											},
										},
										&messaging_api.FlexText{
											Align: "end",
											Size:  "sm",
											Text:  card.Address,
											Action: &messaging_api.UriAction{
												Uri: "https://www.google.com/maps/search/?api=1&query=" + addressEncode + "&openExternalBrowser=1",
											},
										},
										&messaging_api.FlexText{
											Align:  "end",
											Margin: "xxl",
											Text:   card.Phone,
											Action: &messaging_api.UriAction{
												Uri: "tel:" + card.Phone,
											},
										},
										&messaging_api.FlexText{
											Align: "end",
											Text:  card.Email,
											Action: &messaging_api.UriAction{
												Uri: "mailto:" + card.Email,
											},
										},
										&messaging_api.FlexText{
											Align: "end",
											Text:  "更多資訊",
											Action: &messaging_api.UriAction{
												Uri: "https://github.com/kkdai/linebot-smart-namecard",
											},
										},
									},
								},
							},
						},
					}
					cards = append(cards, card)
				}

				contents := &messaging_api.FlexCarousel{
					Contents: cards,
				}

				// Reply message
				if _, err := bot.ReplyMessage(
					&messaging_api.ReplyMessageRequest{
						ReplyToken: e.ReplyToken,
						Messages: []messaging_api.MessageInterface{
							&messaging_api.TextMessage{
								Text: ret,
							},
							&messaging_api.FlexMessage{
								Contents: contents,
								AltText:  "請到手機上查看名片資訊",
							},
						},
					},
				); err != nil {
					log.Print(err)
					return
				}

			// Handle only image messages
			case webhook.ImageMessageContent:
				// 取得用戶 ID
				var uID string
				switch source := e.Source.(type) {
				case webhook.UserSource:
					uID = source.UserId
				case webhook.GroupSource:
					uID = source.UserId
				case webhook.RoomSource:
					uID = source.UserId
				}

				log.Println("Got img msg ID:", message.Id)
				// Get image content through message.Id
				content, err := blob.GetMessageContent(message.Id)
				if err != nil {
					log.Println("Got GetMessageContent err:", err)
				}
				// Read image content
				defer content.Body.Close()
				data, err := io.ReadAll(content.Body)
				if err != nil {
					log.Fatal(err)
				}

				// Pass the image content to the gemini-1.5-flash-latest model for image description
				model := client.GenerativeModel("gemini-1.5-flash-latest")
				prompt := []genai.Part{
					genai.ImageData("png", data),
					genai.Text(ImagePrompt),
				}
				resp, err := model.GenerateContent(ctx, prompt...)
				if err != nil {
					retStr := "無法辨識圖片內容文字，請重新輸入:" + err.Error()
					log.Fatal(err)
					bot.ReplyMessage(
						&messaging_api.ReplyMessageRequest{
							ReplyToken: e.ReplyToken,
							Messages: []messaging_api.MessageInterface{
								&messaging_api.TextMessage{
									Text: retStr,
								},
							},
						},
					)
				}

				// Get the returned content
				var ret string
				for _, cand := range resp.Candidates {
					for _, part := range cand.Content.Parts {
						ret = ret + fmt.Sprintf("%v", part)
						log.Println(part)
					}
				}
				// Print original result.
				log.Println(ret)

				// Remove first and last line,	which are the backticks.
				lines := strings.Split(ret, "\n")
				jsonData := strings.Join(lines[1:len(lines)-1], "\n")
				log.Println("Got jsonData:", jsonData)

				// Parse json and insert NotionDB
				var person Person
				err = json.Unmarshal([]byte(jsonData), &person)
				if err != nil {
					fmt.Println("Unmarshal failed, ", err, "jsonData:", jsonData)
				}

				people := []Person{person}
				var cards []messaging_api.FlexBubble
				for _, card := range people {
					// Get URL encode for company name and address
					companyEncode := url.QueryEscape(card.Company)
					addressEncode := url.QueryEscape(card.Address)

					card := messaging_api.FlexBubble{
						Size: messaging_api.FlexBubbleSIZE_GIGA,
						Body: &messaging_api.FlexBox{
							Layout:  messaging_api.FlexBoxLAYOUT_HORIZONTAL,
							Spacing: "md",
							Contents: []messaging_api.FlexComponentInterface{
								&messaging_api.FlexImage{
									AspectMode:  "cover",
									AspectRatio: "1:1",
									Flex:        1,
									Size:        "full",
									Url:         LogoImageUrl,
								},
								&messaging_api.FlexBox{
									Flex:   4,
									Layout: messaging_api.FlexBoxLAYOUT_VERTICAL,
									Contents: []messaging_api.FlexComponentInterface{
										&messaging_api.FlexText{
											Align:  "end",
											Size:   "xxl",
											Text:   card.Name,
											Weight: "bold",
										},
										&messaging_api.FlexText{
											Align: "end",
											Size:  "sm",
											Text:  card.Title,
										},
										&messaging_api.FlexText{
											Align:  "end",
											Margin: "xxl",
											Size:   "lg",
											Text:   card.Company,
											Weight: "bold",
											Action: &messaging_api.UriAction{
												Uri: "https://www.google.com/maps/search/?api=1&query=" + companyEncode + "&openExternalBrowser=1",
											},
										},
										&messaging_api.FlexText{
											Align: "end",
											Size:  "sm",
											Text:  card.Address,
											Action: &messaging_api.UriAction{
												Uri: "https://www.google.com/maps/search/?api=1&query=" + addressEncode + "&openExternalBrowser=1",
											},
										},
										&messaging_api.FlexText{
											Align:  "end",
											Margin: "xxl",
											Text:   card.Phone,
											Action: &messaging_api.UriAction{
												Uri: "tel:" + card.Phone,
											},
										},
										&messaging_api.FlexText{
											Align: "end",
											Text:  card.Email,
											Action: &messaging_api.UriAction{
												Uri: "mailto:" + card.Email,
											},
										},
										&messaging_api.FlexText{
											Align: "end",
											Text:  "更多資訊",
											Action: &messaging_api.UriAction{
												Uri: "https://github.com/kkdai/linebot-smart-namecard",
											},
										},
									},
								},
							},
						},
					}
					cards = append(cards, card)
				}

				contents := &messaging_api.FlexCarousel{
					Contents: cards,
				}

				// Insert the person data into firebase
				userPath := fmt.Sprintf("%s/%s", DBCardPath, uID)
				_, err = fireDB.NewRef(userPath).Push(ctx, person)
				if err != nil {
					log.Println("Error inserting data into firebase:", err)
				}

				// Reply message
				if _, err := bot.ReplyMessage(
					&messaging_api.ReplyMessageRequest{
						ReplyToken: e.ReplyToken,
						Messages: []messaging_api.MessageInterface{
							&messaging_api.TextMessage{
								Text: jsonData,
							},
							&messaging_api.FlexMessage{
								Contents: contents,
								AltText:  "請到手機上查看名片資訊",
							},
						},
					},
				); err != nil {
					log.Print(err)
					return
				}

			// Handle only video message
			case webhook.VideoMessageContent:
				log.Println("Got video msg ID:", message.Id)

			default:
				log.Printf("Unknown message: %v", message)
			}
		case webhook.FollowEvent:
			log.Printf("message: Got followed event")
		case webhook.PostbackEvent:
			data := e.Postback.Data
			log.Printf("Unknown message: Got postback: " + data)
		case webhook.BeaconEvent:
			log.Printf("Got beacon: " + e.Beacon.Hwid)
		}
	}
}
