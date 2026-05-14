package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/denisK-H/imei-sn_checker/backend/storage"
	"github.com/denisK-H/imei-sn_checker/backend/telegrambot"

	"github.com/joho/godotenv"
)

var lowBalanceAlertFlag bool

type ServiceConfig struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
	Key  string `json:"key"`
}

type requestToIfreeiCloud struct {
	Service string `json:"service"`
	IMEISN  string `json:"imei"`
	Key     string `json:"key"`
}

type requestToCheckBalance struct {
	AccountInfo string `json:"accountinfo"`
	Key         string `json:"key"`
}

type balanceObject struct {
	AccountBalance   float64 `json:"account_balance"`
	AvailableBalance float64 `json:"available_balance"`
}

type balanceResponse struct {
	Success bool          `json:"success"`
	Error   string        `json:"error"`
	Object  balanceObject `json:"object"`
}

type objectResponseByIfreeiCloud struct {
	Image           string `json:"thumbnail"`
	ModelDesc       string `json:"modelDesc"`
	Model           string `json:"model"`
	Network         string `json:"network"`
	Imei            string `json:"imei"`
	Serial          string `json:"serial"`
	WarrantyStatus  string `json:"warrantyStatus"`
	EstPurchaseDate string `json:"estPurchaseDate"`
	AppleRegion     string `json:"apple/region"`
	AppleModelName  string `json:"apple/modelName"`
	UsaBlockStatus  string `json:"usaBlockStatus"`

	TechnicalSupport bool `json:"technicalSupport"`
	RepairCoverage   bool `json:"repairCoverage"`
	Replaced         bool `json:"replaced"`
	Replacement      bool `json:"replacement"`
	Refurbished      bool `json:"refurbished"`
	DemoUnit         bool `json:"demoUnit"`
	Loaner           bool `json:"loaner"`
	FmiOn            bool `json:"fmiOn"`
	IsAppleDevice    bool `json:"isAppleDevice"`
	SimLock          bool `json:"simLock"`

	Imei2 any `json:"imei2"`
}

type fullResponseByIfreeiCloud struct {
	Success bool                        `json:"success"`
	Error   string                      `json:"error"`
	Object  objectResponseByIfreeiCloud `json:"object"`
}

var serviceConfigs []ServiceConfig

type CheckRequest struct {
	Identifier string `json:"identifier"`
	Token      string `json:"token"`
}

type CheckResponse struct {
	Status  string `json:"status"`
	Massage string `json:"massage"`
}

func init() {
	err := godotenv.Load()
	if err != nil {
		log.Println("Info: .env файл не найден, будут использованы системные переменные") //для работы на сервере можно этот блок удалить
	}

	configPath := os.Getenv("CONFIG_FILE")
	if configPath == "" {
		configPath = "./backend/configs/services.json"
	}

	fileData, err := os.ReadFile(configPath)
	if err != nil {
		log.Fatalf("Критическая ошибка: не удалось прочитать файл %s, %v", configPath, err)
	}

	err = json.Unmarshal(fileData, &serviceConfigs)
	if err != nil {
		log.Fatalf("Критическая ошибка: файл %s неправильно форматирован или сломан, %v", configPath, err)
	}

	log.Printf("Успешно: файл %s конфигурация загружена. Найдено сервисов API: %d\n", configPath, len(serviceConfigs))
}

func healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "Успешно запущено")
}

func checkBalance() (float64, error) {
	ifreeiCloudAPI := serviceConfigs[0]

	data := url.Values{}
	data.Set("accountinfo", "balance")
	data.Set("key", ifreeiCloudAPI.Key)

	client := &http.Client{
		Timeout: 25 * time.Second,
	}

	req, err := http.NewRequest(http.MethodPost, ifreeiCloudAPI.URL, strings.NewReader(data.Encode()))
	if err != nil {
		return -1, fmt.Errorf("Ошибка при формировании http запроса: %v", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return -1, fmt.Errorf("Сервер ifreeiCloud недоступен: %v", err)
	}
	defer resp.Body.Close()

	var apiResult balanceResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResult); err != nil {
		return -1, fmt.Errorf("Ошибка чтения ответа: %v", err)
	}

	if !apiResult.Success {
		return -1, fmt.Errorf("Ошибка API: %s", apiResult.Error)
	}

	return apiResult.Object.AvailableBalance, nil
}

func startBalanceMonitor(bot *telegrambot.Bot) {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		<-ticker.C

		balance, err := checkBalance()
		if err != nil {
			log.Printf("Ошибка проверки баланса: %v", err)
			continue
		}

		bot.SetBalance(balance)

		if balance <= 1.0 && !lowBalanceAlertFlag {
			msg := fmt.Sprintf("Внимание! Баланс сервиса критически мал: $%.3f", balance)
			bot.NotifyAdmins(msg)

			lowBalanceAlertFlag = true
			log.Println("Уведомление о низком балансе отправлено админам")
		}

		if balance > 1.0 && lowBalanceAlertFlag {
			msg := fmt.Sprintf("✅ Баланс ifreeiCloud успешно пополнен. Текущий баланс: $%.3f", balance)
			bot.NotifyAdmins(msg)

			lowBalanceAlertFlag = false
			log.Println("Баланс успешно пополнен")
		}
	}
}

func checkIfreeiCloudAPI(identifier string) (*fullResponseByIfreeiCloud, error) {
	ifreeiCloudAPI := serviceConfigs[0]

	data := url.Values{}
	data.Set("service", "205")
	data.Set("imei", identifier)
	data.Set("key", ifreeiCloudAPI.Key)

	client := &http.Client{
		Timeout: 25 * time.Second,
	}

	req, err := http.NewRequest(http.MethodPost, ifreeiCloudAPI.URL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("Ошибка при формировании http запроса: %v", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Сервер ifreeiCloud недоступен: %v", err)
	}
	defer resp.Body.Close()

	log.Printf("Статус от iFree: %d", resp.StatusCode)

	var apiResult fullResponseByIfreeiCloud
	if err := json.NewDecoder(resp.Body).Decode(&apiResult); err != nil {
		return nil, fmt.Errorf("Ошибка чтения ответа: %v", err)
	}
	return &apiResult, nil
}

func checkHandler(s *storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			sendJson(w, "error", "Принимаются только методы POST")
			return
		}

		var req CheckRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			sendJson(w, "error", "Неверный формат данных (ожидается JSON)")
			return
		}

		if req.Token == "" {
			sendJson(w, "error", "Введите личный токен")
			return
		}

		isActive, err := s.ValidateToken(req.Token)
		if err != nil {
			sendJson(w, "error", "Токен не найден")
			log.Printf("Ошибка БД при чтении токена: %v", err)
			return
		}

		if !isActive {
			sendJson(w, "error", "Токен недействителен")
			return
		}

		var deviceType string
		isIMEI, _ := regexp.MatchString(`^\d{15}$`, req.Identifier)
		isSN, _ := regexp.MatchString(`^[A-Za-z0-9]{10,12}$`, req.Identifier)
		if isIMEI {
			deviceType = "IMEI"
		} else if isSN {
			deviceType = "SN"
		} else {
			sendJson(w, "error", "Некорректно введён IMEI/SN. Введите 15 цифр IMEI или 10-12 символов SN")
			return
		}

		log.Printf("Получен запрос на проверку. Тип: %s, значение: %s", deviceType, req.Identifier)

		apiResult, err := checkIfreeiCloudAPI(req.Identifier)
		if err != nil {
			sendJson(w, "error", "Ошибка связи с сервером проверок ")
			return
		}

		if !apiResult.Success {
			sendJson(w, "error", apiResult.Error)
			return
		}

		err = s.IncrementTokenUsage(req.Token)
		if err != nil {
			log.Printf("Ошибка при увеличении счетчика запросов: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")

		finalResponse := map[string]any{
			"status": "success",
			"data":   apiResult.Object,
		}
		json.NewEncoder(w).Encode(finalResponse)
	}
}

func sendJson(w http.ResponseWriter, status, massage string) {
	w.Header().Set("Content-Type", "application/json")
	response := CheckResponse{
		Status:  status,
		Massage: massage,
	}
	json.NewEncoder(w).Encode(response)
}

func main() {
	dbPath := os.Getenv("DATABASE_PATH")
	dbTokens, err := storage.NewStorage(dbPath)
	if err != nil {
		log.Fatalf("Критическая ошибка базы данных: %v", err)
	}
	defer dbTokens.Close()

	tgBot, err := telegrambot.NewBot(dbTokens)
	if err != nil {
		log.Printf("Ошибка инициализации бота: %v", err)
	} else {
		go tgBot.Start()
		log.Printf("Бот успешно запущен")

		go startBalanceMonitor(tgBot)
	}

	http.HandleFunc("/api/health", healthCheckHandler)
	http.HandleFunc("/api/check", checkHandler(dbTokens))

	port := os.Getenv("SERVER_PORT")
	if port == "" {
		port = "8080"
	}

	addr := ":" + port
	log.Printf("Сервер запущен на порту: %s\n", addr)

	err = http.ListenAndServe(addr, nil)
	if err != nil {
		log.Fatalf("Сервер упал с ошибкой: %v", err)
	}
}
