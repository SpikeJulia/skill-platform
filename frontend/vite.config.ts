import { sveltekit } from '@sveltejs/kit/vite';
import tailwindcss from '@tailwindcss/vite';
import { defineConfig } from 'vite';

export default defineConfig({
	plugins: [sveltekit(), tailwindcss()],
	// 注意：不要把 `svelte/attachments` 别名到任何本地 shim。
	// Svelte 运行时只把 description === '@attach' 的 symbol 当作 attachment
	// （见 svelte/src/constants.js 的 ATTACHMENT_KEY 与 spread_attributes 的判定）。
	// 任何返回其它 symbol 的 stub 都会让 bits-ui/svelte-toolbelt 的 attachRef 静默失效，
	// 表现为 Dialog 的 ref 永远为 null、Presence 无法卸载、遮罩层残留并拦截点击。
	server: {
		proxy: {
			'^/(api|_)/': {
				target: 'http://localhost:8090',
				changeOrigin: true,
			},
		},
	},
});
