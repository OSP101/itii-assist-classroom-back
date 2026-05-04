package repositories

import (
	"itii-assist/config"
	"itii-assist/models"
	"itii-assist/utils"
	"strings"
	"time"
)

type ClassroomListParams struct {
	Page        int
	Limit       int
	Search      string
	Building    string
	ShowDeleted string // "true" | "false" | "all"
	SortBy      string
	SortOrder   string
}

type ClassroomListResult struct {
	Classrooms  []ClassroomWithLayout `json:"classrooms"`
	Total       int64                 `json:"total"`
	TotalPages  int                   `json:"total_pages"`
	CurrentPage int                   `json:"current_page"`
	PerPage     int                   `json:"per_page"`
}

type ClassroomWithLayout struct {
	models.Classroom
	Desks []models.Desk `json:"desks"`
	Zones []models.Zone `json:"zones"`
}

type ClassroomStats struct {
	TotalClassrooms   int64 `json:"totalClassrooms"`
	ActiveClassrooms  int64 `json:"activeClassrooms"`
	DeletedClassrooms int64 `json:"deletedClassrooms"`
	TotalDesks        int64 `json:"totalDesks"`
	ComputerDesks     int64 `json:"computerDesks"`
	NormalDesks       int64 `json:"normalDesks"`
	TeacherDesks      int64 `json:"teacherDesks"`
	EnabledDesks      int64 `json:"enabledDesks"`
	DisabledDesks     int64 `json:"disabledDesks"`
	Buildings         int64 `json:"buildings"`
	Total             int64 `json:"total"`
	Active            int64 `json:"active"`
	Deleted           int64 `json:"deleted"`
}

func GetClassrooms(params ClassroomListParams) (ClassroomListResult, error) {
	db := config.DB
	query := db.Model(&models.Classroom{})

	switch params.ShowDeleted {
	case "true":
		query = query.Where("is_deleted = true")
	case "all":
		// no filter
	default:
		query = query.Where("is_deleted = false")
	}

	if params.Search != "" {
		like := "%" + strings.TrimSpace(params.Search) + "%"
		query = query.Where("name ILIKE ? OR building ILIKE ? OR floor ILIKE ?", like, like, like)
	}
	if params.Building != "" {
		query = query.Where("building = ?", params.Building)
	}

	var total int64
	query.Count(&total)

	validCols := map[string]bool{
		"name": true, "building": true, "floor": true, "created_at": true, "updated_at": true,
	}
	col := "created_at"
	if validCols[params.SortBy] {
		col = params.SortBy
	}
	dir := "DESC"
	if strings.ToUpper(params.SortOrder) == "ASC" {
		dir = "ASC"
	}

	if params.Page < 1 {
		params.Page = 1
	}
	if params.Limit < 1 {
		params.Limit = 10
	}
	offset := (params.Page - 1) * params.Limit

	var classrooms []models.Classroom
	if err := query.Order(col + " " + dir).Limit(params.Limit).Offset(offset).Find(&classrooms).Error; err != nil {
		return ClassroomListResult{}, err
	}

	classroomIDs := make([]string, len(classrooms))
	for i, c := range classrooms {
		classroomIDs[i] = c.ID
	}

	var desks []models.Desk
	var zones []models.Zone
	if len(classroomIDs) > 0 {
		db.Where("classroom_id IN ?", classroomIDs).Find(&desks)
		db.Where("classroom_id IN ?", classroomIDs).Find(&zones)
	}

	deskMap := map[string][]models.Desk{}
	for _, d := range desks {
		deskMap[d.ClassroomID] = append(deskMap[d.ClassroomID], d)
	}
	zoneMap := map[string][]models.Zone{}
	for _, z := range zones {
		zoneMap[z.ClassroomID] = append(zoneMap[z.ClassroomID], z)
	}

	result := make([]ClassroomWithLayout, len(classrooms))
	for i, c := range classrooms {
		result[i] = ClassroomWithLayout{
			Classroom: c,
			Desks:     deskMap[c.ID],
			Zones:     zoneMap[c.ID],
		}
	}

	totalPages := int(total) / params.Limit
	if int(total)%params.Limit != 0 {
		totalPages++
	}

	return ClassroomListResult{
		Classrooms:  result,
		Total:       total,
		TotalPages:  totalPages,
		CurrentPage: params.Page,
		PerPage:     params.Limit,
	}, nil
}

func GetClassroomByID(id string) (*ClassroomWithLayout, error) {
	var classroom models.Classroom
	if err := config.DB.First(&classroom, "id = ?", id).Error; err != nil {
		return nil, err
	}

	var desks []models.Desk
	var zones []models.Zone
	config.DB.Where("classroom_id = ?", id).Order("number ASC").Find(&desks)
	config.DB.Where("classroom_id = ?", id).Find(&zones)

	return &ClassroomWithLayout{
		Classroom: classroom,
		Desks:     desks,
		Zones:     zones,
	}, nil
}

func GetClassroomStats() ClassroomStats {
	db := config.DB
	var total, active, deleted, buildings int64
	var totalDesks, computerDesks, normalDesks, teacherDesks, enabledDesks, disabledDesks int64
	db.Model(&models.Classroom{}).Count(&total)
	db.Model(&models.Classroom{}).Where("is_deleted = false AND is_active = true").Count(&active)
	db.Model(&models.Classroom{}).Where("is_deleted = true").Count(&deleted)
	db.Raw("SELECT COUNT(DISTINCT building) FROM classrooms").Scan(&buildings)
	db.Model(&models.Desk{}).Count(&totalDesks)
	db.Model(&models.Desk{}).Where("type = ?", "computer").Count(&computerDesks)
	db.Model(&models.Desk{}).Where("type = ?", "normal").Count(&normalDesks)
	db.Model(&models.Desk{}).Where("type = ?", "teacher").Count(&teacherDesks)
	db.Model(&models.Desk{}).Where("is_enabled = true").Count(&enabledDesks)
	db.Model(&models.Desk{}).Where("is_enabled = false").Count(&disabledDesks)
	return ClassroomStats{
		TotalClassrooms:   total,
		ActiveClassrooms:  active,
		DeletedClassrooms: deleted,
		TotalDesks:        totalDesks,
		ComputerDesks:     computerDesks,
		NormalDesks:       normalDesks,
		TeacherDesks:      teacherDesks,
		EnabledDesks:      enabledDesks,
		DisabledDesks:     disabledDesks,
		Buildings:         buildings,
		Total:             total,
		Active:            active,
		Deleted:           deleted,
	}
}

func CreateClassroom(classroom *models.Classroom) error {
	id, err := utils.GenerateNanoID(21)
	if err != nil {
		return err
	}
	classroom.ID = id
	classroom.CreatedAt = time.Now()
	return config.DB.Create(classroom).Error
}

func UpdateClassroom(classroom *models.Classroom) error {
	return config.DB.Save(classroom).Error
}

func ToggleClassroomStatus(id string) (*models.Classroom, error) {
	var c models.Classroom
	if err := config.DB.First(&c, "id = ?", id).Error; err != nil {
		return nil, err
	}
	c.IsActive = !c.IsActive
	config.DB.Save(&c)
	return &c, nil
}

func SoftDeleteClassroom(id string) error {
	return config.DB.Model(&models.Classroom{}).Where("id = ?", id).Update("is_deleted", true).Error
}

func HardDeleteClassroom(id string) error {
	db := config.DB
	db.Where("classroom_id = ?", id).Delete(&models.Desk{})
	db.Where("classroom_id = ?", id).Delete(&models.Zone{})
	return db.Where("id = ?", id).Delete(&models.Classroom{}).Error
}

func RestoreClassroom(id string) error {
	return config.DB.Model(&models.Classroom{}).Where("id = ?", id).Update("is_deleted", false).Error
}

// UpdateLayout replaces all desks and zones for a classroom with the provided data.
type DeskInput struct {
	ID        string `json:"id"`
	Number    int    `json:"number"`
	X         int    `json:"x"`
	Y         int    `json:"y"`
	Type      string `json:"type"`
	IsEnabled bool   `json:"is_enabled"`
}

type ZoneInput struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	X      int    `json:"x"`
	Y      int    `json:"y"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Color  string `json:"color"`
}

func UpdateLayout(classroomID string, deskInputs []DeskInput, zoneInputs []ZoneInput) error {
	db := config.DB

	// ---- DESKS ----
	var existingDesks []models.Desk
	db.Where("classroom_id = ?", classroomID).Find(&existingDesks)
	existingDeskMap := map[string]bool{}
	for _, d := range existingDesks {
		existingDeskMap[d.ID] = true
	}

	// Collect incoming valid IDs (not temp IDs starting with "desk_")
	incomingIDs := map[string]bool{}
	for _, d := range deskInputs {
		if d.ID != "" && !strings.HasPrefix(d.ID, "desk_") {
			incomingIDs[d.ID] = true
		}
	}

	// Delete removed desks
	for id := range existingDeskMap {
		if !incomingIDs[id] {
			db.Where("id = ?", id).Delete(&models.Desk{})
		}
	}

	// Upsert desks
	for _, d := range deskInputs {
		deskType := d.Type
		if deskType == "" {
			deskType = "normal"
		}
		if d.ID != "" && existingDeskMap[d.ID] {
			db.Model(&models.Desk{}).Where("id = ?", d.ID).Updates(map[string]interface{}{
				"number":     d.Number,
				"x":          d.X,
				"y":          d.Y,
				"type":       deskType,
				"is_enabled": d.IsEnabled,
			})
		} else {
			deskID, err := utils.GenerateNanoID(21)
			if err != nil {
				return err
			}
			db.Create(&models.Desk{
				ID:          deskID,
				ClassroomID: classroomID,
				Number:      d.Number,
				X:           d.X,
				Y:           d.Y,
				Type:        deskType,
				IsEnabled:   d.IsEnabled,
			})
		}
	}

	// ---- ZONES ----
	var existingZones []models.Zone
	db.Where("classroom_id = ?", classroomID).Find(&existingZones)
	existingZoneMap := map[string]bool{}
	for _, z := range existingZones {
		existingZoneMap[z.ID] = true
	}

	incomingZoneIDs := map[string]bool{}
	for _, z := range zoneInputs {
		if z.ID != "" && !strings.HasPrefix(z.ID, "zone_") {
			incomingZoneIDs[z.ID] = true
		}
	}

	for id := range existingZoneMap {
		if !incomingZoneIDs[id] {
			db.Where("id = ?", id).Delete(&models.Zone{})
		}
	}

	for _, z := range zoneInputs {
		color := z.Color
		if color == "" {
			color = "rgba(99,102,241,0.15)"
		}
		if z.ID != "" && existingZoneMap[z.ID] {
			db.Model(&models.Zone{}).Where("id = ?", z.ID).Updates(map[string]interface{}{
				"name":   z.Name,
				"x":      z.X,
				"y":      z.Y,
				"width":  z.Width,
				"height": z.Height,
				"color":  color,
			})
		} else {
			zoneID, err := utils.GenerateNanoID(21)
			if err != nil {
				return err
			}
			db.Create(&models.Zone{
				ID:          zoneID,
				ClassroomID: classroomID,
				Name:        z.Name,
				X:           z.X,
				Y:           z.Y,
				Width:       z.Width,
				Height:      z.Height,
				Color:       color,
			})
		}
	}

	return nil
}
