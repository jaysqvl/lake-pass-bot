package web

import (
	"strings"

	"github.com/jaysqvl/lake-pass-bot/internal/destinations"
	"github.com/jaysqvl/lake-pass-bot/internal/model"
)

func lakeName(id string) string {
	lake, err := destinations.Resolve(id)
	if err != nil {
		return "Unsupported lake"
	}
	return lake.Name
}

func passOptionLabel(pass string) string {
	switch model.PassType(pass) {
	case model.PassAllDay:
		return "All-day"
	case model.PassAfternoon:
		return "Afternoon"
	case model.PassMorning:
		return "Morning"
	default:
		return strings.ReplaceAll(pass, "_", " ")
	}
}

func passNames(values []model.PassType) []string {
	result := make([]string, len(values))
	for i, value := range values {
		switch value {
		case model.PassAllDay:
			result[i] = "All-day"
		case model.PassMorning:
			result[i] = "Morning"
		case model.PassAfternoon:
			result[i] = "Afternoon"
		default:
			result[i] = strings.ReplaceAll(string(value), "_", " ")
		}
	}
	return result
}
