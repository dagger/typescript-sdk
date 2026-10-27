// A module's copy of client.gen.ts imports the runtime from "./core.js", the
// library bundle beside it. Inside the library's own source the same specifier
// reaches the session runtime here, so the generated bindings read the same in
// both places and the library's file can be shipped into a module as it is.
export { Context, BaseClient } from "../session/context.js"
