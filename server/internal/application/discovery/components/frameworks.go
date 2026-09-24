package components

// frameworkNames maps a dependency (npm package, Go module path, or a
// generic ecosystem keyword) to the display name recorded in the component
// stack. Only the names that decide how the code is written earn a place
// here.
var frameworkNames = map[string]string{
	"next":             "Next.js",
	"nuxt":             "Nuxt",
	"@remix-run/react": "Remix",
	"@remix-run/node":  "Remix",
	"gatsby":           "Gatsby",
	"@sveltejs/kit":    "SvelteKit",
	"@angular/core":    "Angular",
	"astro":            "Astro",
	"solid-js":         "Solid",
	"vue":              "Vue",
	"svelte":           "Svelte",
	"preact":           "Preact",
	"react":            "React",
	"react-dom":        "React",
	"vite":             "Vite",
	"webpack":          "Webpack",
	"parcel":           "Parcel",
	"electron":         "Electron",
	"@tauri-apps/api":  "Tauri",
	"@tauri-apps/cli":  "Tauri",

	"@nestjs/core":          "NestJS",
	"express":               "Express",
	"fastify":               "Fastify",
	"koa":                   "Koa",
	"@hapi/hapi":            "hapi",
	"hono":                  "Hono",
	"@trpc/server":          "tRPC",
	"apollo-server":         "Apollo Server",
	"apollo-server-express": "Apollo Server",

	"fastapi":   "FastAPI",
	"django":    "Django",
	"flask":     "Flask",
	"starlette": "Starlette",
	"aiohttp":   "aiohttp",
	"sanic":     "Sanic",

	"spring-boot": "Spring Boot",
	"ktor":        "Ktor",
	"quarkus":     "Quarkus",
	"micronaut":   "Micronaut",

	"rails":   "Rails",
	"sinatra": "Sinatra",

	"laravel/framework":        "Laravel",
	"symfony/framework-bundle": "Symfony",

	"axum":      "axum",
	"actix-web": "actix-web",
	"rocket":    "Rocket",
	"warp":      "warp",

	"flutter":      "Flutter",
	"expo":         "Expo",
	"react-native": "React Native",

	"github.com/go-chi/chi/v5":      "chi",
	"github.com/gin-gonic/gin":      "Gin",
	"github.com/labstack/echo/v4":   "Echo",
	"github.com/gofiber/fiber/v2":   "Fiber",
	"github.com/gorilla/mux":        "gorilla/mux",
	"connectrpc.com/connect":        "Connect",
	"google.golang.org/grpc":        "gRPC",
	"github.com/go-chi/huma":        "Huma",
	"github.com/danielgtaylor/huma": "Huma",

	"Microsoft.AspNetCore.App": "ASP.NET Core",
	"Microsoft.AspNetCore":     "ASP.NET Core",
}

// libraryNames is the curated, ≤8-item slice of dependencies worth naming as
// libraries rather than frameworks: data access, styling and test tooling
// that changes how the code is written without deciding its shape.
var libraryNames = map[string]string{
	"prisma":                            "Prisma",
	"drizzle-orm":                       "Drizzle",
	"typeorm":                           "TypeORM",
	"mongoose":                          "Mongoose",
	"sequelize":                         "Sequelize",
	"github.com/jackc/pgx/v5":           "pgx",
	"github.com/jackc/pgx":              "pgx",
	"gorm.io/gorm":                      "GORM",
	"sqlalchemy":                        "SQLAlchemy",
	"alembic":                           "Alembic",
	"tailwindcss":                       "Tailwind CSS",
	"@tanstack/react-query":             "TanStack Query",
	"zustand":                           "Zustand",
	"redux":                             "Redux",
	"vitest":                            "Vitest",
	"jest":                              "Jest",
	"playwright":                        "Playwright",
	"@playwright/test":                  "Playwright",
	"cypress":                           "Cypress",
	"pytest":                            "pytest",
	"github.com/stretchr/testify":       "testify",
	"github.com/smacker/go-tree-sitter": "tree-sitter",
}

const radixPrefix = "@radix-ui/"

func frameworkDisplayName(dep string) (string, bool) {
	name, ok := frameworkNames[dep]
	return name, ok
}

func libraryDisplayName(dep string) (string, bool) {
	if name, ok := libraryNames[dep]; ok {
		return name, true
	}
	if len(dep) > len(radixPrefix) && dep[:len(radixPrefix)] == radixPrefix {
		return "Radix UI", true
	}
	return "", false
}
