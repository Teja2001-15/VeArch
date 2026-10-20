package handlers

import (
	"html/template"
	"log"
	"net/http"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"arch_erp/internal/auth"
	"arch_erp/internal/db"
	"arch_erp/internal/models"
)

// CalendarDay is one visible cell in the month grid. Adjacent-month days are
// included so every row has seven days and the calendar never has ragged edges.
type CalendarDay struct {
	Date           string
	DayNumber      int
	IsCurrentMonth bool
	IsToday        bool
	Tasks          []models.Task
}

type CalendarHandler struct{ tmpls map[string]*template.Template }

func NewCalendarHandler(tmpls map[string]*template.Template) *CalendarHandler {
	return &CalendarHandler{tmpls: tmpls}
}

// CalendarView renders a selected month of task deadlines. The optional month
// query parameter uses YYYY-MM; invalid values fall back to the current month.
func (h *CalendarHandler) CalendarView(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	monthStart := requestedMonth(r.URL.Query().Get("month"), time.Now().UTC())
	nextMonth := monthStart.AddDate(0, 1, 0)

	taskFilter := bson.M{"deadline": bson.M{"$gte": monthStart, "$lt": nextMonth}}
	if user.Role != models.RoleAdmin && user.Role != models.RoleSeniorArchitect {
		taskFilter["assignee_id"] = user.ID
	}

	cursor, err := db.Collections.Tasks.Find(r.Context(), taskFilter, options.Find().SetSort(bson.D{{Key: "deadline", Value: 1}}))
	if err != nil {
		log.Println("error fetching calendar tasks:", err)
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	defer cursor.Close(r.Context())
	var tasks []models.Task
	if err := cursor.All(r.Context(), &tasks); err != nil {
		log.Println("error reading calendar tasks:", err)
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	projects, err := calendarProjects(r)
	if err != nil {
		log.Println("error fetching calendar projects:", err)
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	projectMap := make(map[string]models.Project, len(projects))
	for _, project := range projects {
		projectMap[project.ID.Hex()] = project
	}

	tasksByDate := make(map[string][]models.Task)
	for _, task := range tasks {
		if task.Deadline != nil {
			dateKey := task.Deadline.UTC().Format(time.DateOnly)
			tasksByDate[dateKey] = append(tasksByDate[dateKey], task)
		}
	}

	data := map[string]any{
		"Title":         "Calendar — Arch ERP",
		"ActivePage":    "calendar",
		"CurrentUser":   user,
		"ProjectMap":    projectMap,
		"Days":          buildCalendarDays(monthStart, tasksByDate, time.Now().UTC()),
		"MonthName":     monthStart.Month().String(),
		"Year":          monthStart.Year(),
		"PreviousMonth": monthStart.AddDate(0, -1, 0).Format("2006-01"),
		"NextMonth":     nextMonth.Format("2006-01"),
	}
	if err := h.tmpls["calendar.html"].ExecuteTemplate(w, "calendar.html", data); err != nil {
		log.Println("calendar template error:", err)
	}
}

func calendarProjects(r *http.Request) ([]models.Project, error) {
	cursor, err := db.Collections.Projects.Find(r.Context(), bson.M{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(r.Context())
	var projects []models.Project
	return projects, cursor.All(r.Context(), &projects)
}

func requestedMonth(value string, fallback time.Time) time.Time {
	if value != "" {
		if parsed, err := time.Parse("2006-01", value); err == nil && parsed.Format("2006-01") == value {
			return parsed.UTC()
		}
	}
	return time.Date(fallback.Year(), fallback.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func buildCalendarDays(monthStart time.Time, tasksByDate map[string][]models.Task, today time.Time) []CalendarDay {
	gridStart := monthStart.AddDate(0, 0, -int(monthStart.Weekday()))
	days := make([]CalendarDay, 0, 42) // Six full weeks prevents layout jumps between months.
	for offset := 0; offset < 42; offset++ {
		date := gridStart.AddDate(0, 0, offset)
		dateKey := date.Format(time.DateOnly)
		days = append(days, CalendarDay{
			Date:           dateKey,
			DayNumber:      date.Day(),
			IsCurrentMonth: date.Month() == monthStart.Month(),
			IsToday:        dateKey == today.UTC().Format(time.DateOnly),
			Tasks:          tasksByDate[dateKey],
		})
	}
	return days
}
