package telegrambot

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/denisK-H/imei-sn_checker/backend/storage"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Bot struct {
	api      *tgbotapi.BotAPI
	store    *storage.Storage
	adminIDs map[int64]bool
	sessions map[int64]*Session

	balanceMutex   sync.RWMutex
	currentBalance float64
}

type Session struct {
	step       string
	telegramID string
}

const limitOnPage = 6

func NewBot(s *storage.Storage) (*Bot, error) {
	botAPI := os.Getenv("TELEGRAM_BOT_API")
	if botAPI == "" {
		return nil, fmt.Errorf("Не добавлен API ключ телеграм бота")
	}

	adminIDSstr := os.Getenv("ADMIN_TG_IDS")
	if adminIDSstr == "" {
		return nil, fmt.Errorf("Не добавлены telegram ID администраторов")
	}
	adminIDs := make(map[int64]bool)

	for _, idStr := range strings.Split(adminIDSstr, ",") {
		idStr = strings.TrimSpace(idStr)
		if idStr == "" {
			continue
		}

		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("Ошибка парсинга ID администратора (%s): %v", idStr, err)
		}

		adminIDs[id] = true
	}

	api, err := tgbotapi.NewBotAPI(botAPI)
	if err != nil {
		return nil, fmt.Errorf("Ошибка авторизации бота: %v", err)
	}
	log.Printf("Бот авторизован под именем %s", api.Self.UserName)

	commands := tgbotapi.NewSetMyCommands(
		tgbotapi.BotCommand{
			Command:     "start",
			Description: "Let's start use",
		},
		tgbotapi.BotCommand{
			Command:     "new",
			Description: "Создать новый токен",
		},
		tgbotapi.BotCommand{
			Command:     "list",
			Description: "Показать все токены (активные и деактивированные)",
		},
		tgbotapi.BotCommand{
			Command:     "balance",
			Description: "Проверить баланс",
		})
	if _, err := api.Request(commands); err != nil {
		log.Printf("Ошибка при установки команд в меню :%v", err)
	}

	return &Bot{
		api:      api,
		store:    s,
		adminIDs: adminIDs,
		sessions: make(map[int64]*Session),
	}, nil
}

func (b *Bot) SetBalance(balance float64) {
	b.balanceMutex.Lock()
	defer b.balanceMutex.Unlock()
	b.currentBalance = balance
}

func (b *Bot) GetBalance() float64 {
	b.balanceMutex.RLock()
	defer b.balanceMutex.RUnlock()
	return b.currentBalance
}

func (b *Bot) Start() {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.api.GetUpdatesChan(u)

	for update := range updates {
		if update.CallbackQuery != nil {
			if !b.adminIDs[update.CallbackQuery.From.ID] {
				continue
			}
			b.handleCallback(update.CallbackQuery)
			continue
		}

		if update.Message == nil || update.Message.Text == "" {
			continue
		}

		if !b.adminIDs[update.Message.From.ID] {
			b.sendMessage(update.Message.Chat.ID, "Вы не являетесь администратором, обратитесь к @IlshatBayanov за получением токена")
			continue
		}

		if update.Message.IsCommand() {
			b.handleCommand(update.Message)
		} else {
			b.handleDialog(update.Message)
		}
	}
}

func (b *Bot) handleCallback(query *tgbotapi.CallbackQuery) {
	callback := tgbotapi.NewCallback(query.ID, "")
	b.api.Request(callback)
	data := query.Data
	if data == "cancel" {
		delete(b.sessions, query.Message.From.ID)

		edit := tgbotapi.NewEditMessageTextAndMarkup(query.Message.Chat.ID,
			query.Message.MessageID,
			"🛑 Создание токена отменено",
			tgbotapi.InlineKeyboardMarkup{InlineKeyboard: make([][]tgbotapi.InlineKeyboardButton, 0)},
		)
		b.api.Send(edit)
		return
	}

	if strings.HasPrefix(data, "page_") {
		pageStr := strings.TrimPrefix(data, "page_")
		page, _ := strconv.Atoi(pageStr)

		tokens, _ := b.store.GetAllTokens()

		text := "Список токенов, нажмите на токен для просмотра подробной информации"

		edit := tgbotapi.NewEditMessageTextAndMarkup(query.Message.Chat.ID, query.Message.MessageID, text, buildPaginationKeyboard(tokens, page))
		b.api.Send(edit)
		return
	}

	if strings.HasPrefix(data, "revoke_") {
		idStr := strings.TrimPrefix(data, "revoke_")
		id, _ := strconv.Atoi(idStr)
		tokenData, err := b.store.GetTokenById(id)
		if err == nil {
			b.store.RevokeToken(tokenData.Token)
			data = "info_" + idStr
		}
	} else if strings.HasPrefix(data, "activate_") {
		idStr := strings.TrimPrefix(data, "activate_")
		id, _ := strconv.Atoi(idStr)
		tokenData, err := b.store.GetTokenById(id)
		if err == nil {
			b.store.ActivateToken(tokenData.Token)
			data = "info_" + idStr
		}
	}

	if strings.HasPrefix(data, "info_") {
		idStr := strings.TrimPrefix(data, "info_")
		id, _ := strconv.Atoi(idStr)

		tokenData, err := b.store.GetTokenById(id)
		if err != nil {
			return
		}
		status := "🟢 АКТИВЕН"
		if !tokenData.IsActive {
			status = "🔴 ЗАБЛОКИРОВАН"
		}

		text := fmt.Sprintf(
			"Информация о токене:\n\n"+
				"id: %d\n"+
				"Токен: %s\n"+
				"Telegram ID: %s\n"+
				"Комментарий: %s\n"+
				"Количество запросов: %d\n"+
				"Статус: %s\n"+
				"Дата создания: %s\n",
			tokenData.Id, tokenData.Token, tokenData.TelegramID, tokenData.Label, tokenData.CountRequest, status, tokenData.CreatedAt,
		)

		var actionBtn tgbotapi.InlineKeyboardButton
		if tokenData.IsActive {
			actionBtn = tgbotapi.NewInlineKeyboardButtonData("🔴 Деактивировать", fmt.Sprintf("revoke_%d", tokenData.Id))
		} else {
			actionBtn = tgbotapi.NewInlineKeyboardButtonData("🟢 Активировать", fmt.Sprintf("activate_%d", tokenData.Id))
		}

		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(actionBtn), // Наша новая кнопка действий
			tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("🔙 Вернуться к списку", "page_0")),
		)

		edit := tgbotapi.NewEditMessageTextAndMarkup(query.Message.Chat.ID, query.Message.MessageID, text, keyboard)
		b.api.Send(edit)
		return
	}
}

func (b *Bot) handleDialog(message *tgbotapi.Message) {
	session, exists := b.sessions[message.Chat.ID]
	if !exists {
		return
	}

	text := strings.TrimSpace(message.Text)

	switch session.step {
	case "wait_id":
		session.telegramID = text

		session.step = "wait_label"
		msg := tgbotapi.NewMessage(message.Chat.ID, "Теперь введите комментарий для этого токена")
		msg.ReplyMarkup = CancelButton()
		_, err := b.api.Send(msg)
		if err != nil {
			log.Printf("Ошибка при отправке сообщения (%s), в чат %d : %v", message, message.Chat.ID, err)
		}

	case "wait_label":
		label := text

		newToken, err := storage.GenerateToken()
		if err != nil {
			b.sendMessage(message.Chat.ID, "Ошибка генерации токена"+err.Error())
			delete(b.sessions, message.Chat.ID)
			return
		}

		err = b.store.AddToken(newToken, session.telegramID, label)
		if err != nil {
			b.sendMessage(message.Chat.ID, "Ошибка добавления токена"+err.Error())
			delete(b.sessions, message.Chat.ID)
			return
		}

		reply := fmt.Sprintf("✅ Токен успешно создан, telegramID: %s, комментарий: %s, токен: %s", session.telegramID, label, newToken)
		b.sendMessage(message.Chat.ID, reply)

		delete(b.sessions, message.Chat.ID)
	}
}

func (b *Bot) handleCommand(message *tgbotapi.Message) {
	command := message.Command()
	switch command {
	case "start":
		b.sendMessage(message.Chat.ID,
			`Привет, это панель администратора. Доступные команды:
/new - Создать новый токен
/list - Показать все токены (активные и деактивированные)
/balance - Показать баланс (обновляется раз в 10 минут)`)

	case "new":
		b.sessions[message.Chat.ID] = &Session{step: "wait_id"}
		msg := tgbotapi.NewMessage(message.Chat.ID, "Введите Telegram ID пользователя")
		msg.ReplyMarkup = CancelButton()
		_, err := b.api.Send(msg)
		if err != nil {
			log.Printf("Ошибка при отправке сообщения (%s), в чат %d : %v", msg.Text, message.Chat.ID, err)
		}

	case "list":
		tokens, err := b.store.GetAllTokens()
		if err != nil || len(tokens) == 0 {
			b.sendMessage(message.Chat.ID, "База данных с токенами пуста")
			return
		}
		msg := tgbotapi.NewMessage(message.Chat.ID, "Список токенов, нажмите на токен для просмотра подробной информации")
		msg.ReplyMarkup = buildPaginationKeyboard(tokens, 0)
		_, err = b.api.Send(msg)
		if err != nil {
			log.Printf("Ошибка при отправке сообщения (%s), в чат %d : %v", msg.Text, message.Chat.ID, err)
		}

	case "balance":
		bal := b.GetBalance()
		msg := fmt.Sprintf("Текущий баланс: $%.3f (Обновляется раз в 10 минут)", bal)
		b.sendMessage(message.Chat.ID, msg)
	}
}

func (b *Bot) NotifyAdmins(message string) {
	for adminId, isAdmin := range b.adminIDs {
		if isAdmin {
			b.sendMessage(adminId, message)
		}
	}
}

func (b *Bot) sendMessage(chatID int64, message string) {
	msg := tgbotapi.NewMessage(chatID, message)
	_, err := b.api.Send(msg)
	if err != nil {
		log.Printf("Ошибка при отправке сообщения (%s), в чат %d : %v", message, chatID, err)
	}
}

func CancelButton() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("❌ Отмена", "cancel")))
}

func buildPaginationKeyboard(tokens []storage.TokenData, page int) tgbotapi.InlineKeyboardMarkup {
	start := limitOnPage * page
	end := start + limitOnPage

	if start > len(tokens) {
		start = len(tokens)
	}

	if end > len(tokens) {
		end = len(tokens)
	}

	var rows [][]tgbotapi.InlineKeyboardButton

	for _, t := range tokens[start:end] {
		status := "🟢"
		if !t.IsActive {
			status = "🔴"
		}

		btnText := fmt.Sprintf("%s %d, %s, %s", status, t.Id, t.TelegramID, t.Label)
		btnData := fmt.Sprintf("info_%d", t.Id)

		rows = append(rows, tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData(btnText, btnData)))
	}

	var navRow []tgbotapi.InlineKeyboardButton
	if page > 0 {
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData("⬅️ Прошл лист"+strconv.Itoa(page-1), fmt.Sprintf("page_%d", page-1)))
	}
	if end < len(tokens) {
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData("След лист ➡️"+strconv.Itoa(page+1), fmt.Sprintf("page_%d", page+1)))
	}

	if len(navRow) > 0 {
		rows = append(rows, navRow)
	}

	return tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
}
