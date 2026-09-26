const compilerApiTypeScriptVersion = "6.0.3";

/**
 * pnpm install hooks. `readPackage` gives `@hey-api/openapi-ts` its own
 * TypeScript 6 in place of the project's TypeScript 7 peer, because the client
 * generator calls the TypeScript compiler API that TypeScript 7 no longer ships.
 */
export const hooks = {
	readPackage(manifest) {
		if (manifest.name === "@hey-api/openapi-ts") {
			delete manifest.peerDependencies.typescript;
			manifest.dependencies.typescript = compilerApiTypeScriptVersion;
		}
		return manifest;
	},
};
