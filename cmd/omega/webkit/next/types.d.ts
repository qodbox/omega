// TypeScript 6 wants a declaration for side-effect CSS imports, which
// Next 15 does not ship yet.
declare module "*.css" {
  const content: string
  export default content
}
