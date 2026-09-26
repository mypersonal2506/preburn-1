import { defineConfig } from "@hey-api/openapi-ts";

export default defineConfig({
	input: "../api/openapi.json",
	output: "src/client",
	plugins: [
		{
			name: "@hey-api/client-fetch",
			runtimeConfigPath: "./src/lib/api-client",
		},
		"@hey-api/typescript",
		"@hey-api/sdk",
		"zod",
		"@tanstack/react-query",
	],
});
