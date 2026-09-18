package routes

import (
	"net/http"

	"gridwise/internal/controllers"
)

func Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /optimize-energy", controllers.HandleEnergyOptimize)
}
