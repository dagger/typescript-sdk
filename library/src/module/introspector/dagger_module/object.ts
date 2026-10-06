import ts from "typescript"

import { TypeDefKind } from "../../../core/client.gen.js"
import { IntrospectionError } from "../../../session/errors/index.js"
import { AST, Location } from "../typescript_module/index.js"
import { DaggerConstructor } from "./constructor.js"
import {
  COLLECTION_DECORATOR,
  DELTA_DECORATOR,
  FUNCTION_DECORATOR,
  GET_DECORATOR,
  KEYS_DECORATOR,
  OBJECT_DECORATOR,
} from "./decorator.js"
import { DaggerFunction, DaggerFunctions } from "./function.js"
import { Locatable } from "./locatable.js"
import { DaggerObjectBase } from "./objectBase.js"
import { DaggerProperties, DaggerProperty } from "./property.js"
import { References } from "./reference.js"

/**
 * Represents an object defined using the `class` keyword.
 *
 * The class may contains methods and fields, that may or may not be exposed to the Dagger API.
 *
 * @example
 * ```ts
 * type MyObject = {
 *   name: string
 *   age: number
 * }
 * ```
 */
export class DaggerObject extends Locatable implements DaggerObjectBase {
  public isCollection: boolean
  public name: string
  public description: string
  public deprecated?: string
  public _constructor: DaggerConstructor | undefined = undefined
  public methods: DaggerFunctions = {}
  public properties: DaggerProperties = {}
  public isExported: boolean = false
  public isDefaultExport: boolean = false

  private symbol: ts.Symbol

  kind(): "class" | "object" {
    return "class"
  }

  constructor(
    private readonly node: ts.ClassDeclaration,
    private readonly ast: AST,
  ) {
    super(node)

    if (!this.node.name) {
      throw new IntrospectionError(
        `could not resolve name of class at ${AST.getNodePosition(node)}.`,
      )
    }
    this.name = this.node.name.getText()

    this.isCollection = this.ast.isNodeDecoratedWith(node, COLLECTION_DECORATOR)
    if (
      !this.isCollection &&
      !this.ast.isNodeDecoratedWith(node, OBJECT_DECORATOR)
    ) {
      throw new IntrospectionError(
        `class ${this.name} at ${AST.getNodePosition(node)} is used by the module but not exposed with a dagger decorator.`,
      )
    }

    const modifiers = ts.getCombinedModifierFlags(this.node)
    this.isExported = (modifiers & ts.ModifierFlags.Export) !== 0
    // `export default class Foo` sets both Export and Default; the entrypoint
    // must import it as a default import rather than a named one.
    this.isDefaultExport = (modifiers & ts.ModifierFlags.Default) !== 0

    if (!this.isExported) {
      console.warn(
        `missing export in class ${this.name} at ${AST.getNodePosition(node)} but it's used by the module.`,
      )
    }

    this.symbol = this.ast.getSymbolOrThrow(this.node.name)
    const { description, deprecated } = this.ast.getSymbolDoc(this.symbol)
    this.description = description
    this.deprecated = deprecated

    for (const member of this.node.members) {
      if (ts.isPropertyDeclaration(member)) {
        const property = new DaggerProperty(member, this.ast)
        this.properties[property.alias ?? property.name] = property

        continue
      }

      if (ts.isConstructorDeclaration(member)) {
        this._constructor = new DaggerConstructor(member, this.ast)

        continue
      }

      if (
        ts.isMethodDeclaration(member) &&
        (this.ast.isNodeDecoratedWith(member, FUNCTION_DECORATOR) ||
          this.ast.isNodeDecoratedWith(member, GET_DECORATOR))
      ) {
        const daggerFunction = new DaggerFunction(member, this.ast)
        this.methods[daggerFunction.alias ?? daggerFunction.name] =
          daggerFunction

        continue
      }
    }

    if (this.isCollection) {
      this.validateCollection()
    }
  }

  /**
   * Reject a collection the engine would reject anyway.
   *
   * It validates the same shape when the typedefs reach it, but by then the
   * failure is a GraphQL error carrying no source position — the author gets a
   * response dump instead of the member they got wrong.
   *
   * A keys field whose type is still a reference is left to the engine: the
   * list check cannot run before the reference resolves, and guessing here
   * would reject the valid `@keys() names: Names` behind `type Names = string[]`.
   */
  private validateCollection(): void {
    const position = AST.getNodePosition(this.node)

    const keys = Object.values(this.properties).filter((p) => p.isCollectionKeys)
    if (keys.length === 0) {
      throw new IntrospectionError(
        `collection ${this.name} at ${position} requires a field decorated with ${KEYS_DECORATOR}().`,
      )
    }
    if (keys.length > 1) {
      throw new IntrospectionError(
        `collection ${this.name} at ${position} has multiple ${KEYS_DECORATOR}() fields: ${keys.map((k) => k.name).join(", ")}.`,
      )
    }
    if (keys[0].type && keys[0].type.kind !== TypeDefKind.ListKind) {
      throw new IntrospectionError(
        `${KEYS_DECORATOR}() field ${keys[0].name} at ${position} must be a list.`,
      )
    }

    const getters = Object.values(this.methods).filter((m) => m.isCollectionGet)
    if (getters.length === 0) {
      throw new IntrospectionError(
        `collection ${this.name} at ${position} requires a method decorated with ${GET_DECORATOR}().`,
      )
    }
    if (getters.length > 1) {
      throw new IntrospectionError(
        `collection ${this.name} at ${position} has multiple ${GET_DECORATOR}() methods: ${getters.map((g) => g.name).join(", ")}.`,
      )
    }
    if (Object.keys(getters[0].arguments).length !== 1) {
      throw new IntrospectionError(
        `${GET_DECORATOR}() method ${getters[0].name} at ${position} must take exactly one argument.`,
      )
    }

    const deltas = Object.values(this.properties).filter(
      (p) => p.isCollectionDelta,
    )
    if (deltas.length > 1) {
      throw new IntrospectionError(
        `collection ${this.name} at ${position} has multiple ${DELTA_DECORATOR}() fields: ${deltas.map((d) => d.name).join(", ")}.`,
      )
    }
  }

  public getLocation(): Location {
    return AST.getNodeLocation(this.node)
  }

  public getReferences(): string[] {
    const references: string[] = []

    if (this._constructor) {
      references.push(...this._constructor.getReferences())
    }

    for (const property of Object.values(this.properties)) {
      const ref = property.getReference()
      if (ref) {
        references.push(ref)
      }
    }

    for (const fn of Object.values(this.methods)) {
      references.push(...fn.getReferences())
    }

    return references.filter((v, i, arr) => arr.indexOf(v) === i)
  }

  public propagateReferences(references: References): void {
    if (this._constructor) {
      this._constructor.propagateReferences(references)
    }

    for (const property of Object.values(this.properties)) {
      property.propagateReferences(references)
    }

    for (const fn of Object.values(this.methods)) {
      fn.propagateReferences(references)
    }
  }

  public toJSON() {
    return {
      name: this.name,
      description: this.description,
      deprecated: this.deprecated,
      constructor: this._constructor,
      methods: this.methods,
      properties: this.properties,
    }
  }
}
