package hooks

import (
	"github.com/pocketbase/pocketbase/core"
)

// SkillsDir 是中央源路径。
// 容器里走 /skills（docker run -v ~/AI/agent-skills:/skills:ro）
// 宿主机直接跑：环境变量 SKILLS_DIR 覆盖
const SkillsDir = "/skills"

// Register 注册所有自定义 HTTP 路由
func Register(app core.App) {
	// LLM 调用需要一个全局 app 句柄来读配置（原来的模型配置是编译期常量，不需要）
	SetApp(app)

	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		// ===== 静态资源 (SvelteKit 构建产物) =====
		// 已在 main.go 里通过 serveStatic 注册

		// ===== /api/skills — skill CRUD =====
		se.Router.GET("/api/skills", listSkills)
		se.Router.GET("/api/skills/{name}", getSkill)
		se.Router.DELETE("/api/skills/{name}", deleteSkill)

		// ===== /api/unmanaged — 平台管不到、但确实存在的 skill =====
		se.Router.GET("/api/unmanaged", listUnmanaged)

		// ===== /api/install — 安装 =====
		se.Router.POST("/api/install/content", installFromContent)
		se.Router.POST("/api/install/url", installFromURL)

		// ===== /api/find — 语义搜（调 LLM） =====
		se.Router.POST("/api/find", findSkills)

		// ===== /api/agents — agent tab 管理（v0.2.1）=====
		se.Router.GET("/api/agents", listAgents)
		se.Router.POST("/api/agents", createAgent)
		se.Router.PATCH("/api/agents/{name}", updateAgent)
		se.Router.DELETE("/api/agents/{name}", deleteAgent)

		// ===== /api/agent — LLM 驱动的 4 类操作 =====
		se.Router.POST("/api/agent/compose", agentCompose)
		se.Router.POST("/api/agent/orchestrate", agentOrchestrate)
		se.Router.POST("/api/agent/merge", agentMerge)
		se.Router.POST("/api/agent/evolve", agentEvolve)

		// ===== /api/config — agent 的模型与提示词配置 =====
		se.Router.GET("/api/config", getConfig)
		se.Router.PUT("/api/config", putConfig)
		se.Router.POST("/api/config/reset", resetConfig)
		se.Router.GET("/api/config/formats", listAPIFormats)

		return se.Next()
	})
}
