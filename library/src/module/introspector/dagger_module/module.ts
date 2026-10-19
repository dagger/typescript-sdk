import Module from "node:module"
import ts from "typescript"

import { TypeDefKind } from "../../../core/client.gen.js"
import { IntrospectionError } from "../../../session/errors/index.js"
import { AST, ResolvedNodeWithSymbol } from "../typescript_module/index.js"
import {
  COLLECTION_DECORATOR,
  ENUM_DECORATOR,
  OBJECT_DECORATOR,
} from "./decorator.js"
import { DaggerEnum } from "./enum.js"
import { DaggerEnumsBase } from "./enumBase.js"
import { DaggerEnumClass } from "./enumClass.js"
import { DaggerInterface, DaggerInterfaces } from "./interface.js"
import { DaggerObject } from "./object.js"
import { DaggerObjectsBase } from "./objectBase.js"
import { References } from "./reference.js"
import { DaggerTypeObject } from "./typeObject.js"

function declarationKind(node: ts.Declaration): string {
  if (ts.isClassDeclaration(node)) {
    return "class"
  }
  if (ts.isEnumDeclaration(node)) {
    return "enum"
  }
  if (ts.isInterfaceDeclaration(node)) {
    return "interface"
  }

  return "type alias"
}

/**
 * DaggerModule represents a TypeScript module with a set of files
 * with Dagger object conversion and notation.
 *
 * It starts from the module entrypoint (the class named the same as the module) and
 * then recursively resolve every references to other declared identifiers.
 * After resolution, it propagates all the references to the modules declarations
 * and finally it generates the final Dagger module representation.
 */
export class DaggerModule {
  /**
   * An object is either a decorated class or a type alias object.
   * Type alias objects cannot be decorated so they are resolved if referenced in the module.
   *
   * @example
   * ```ts
   * @object()
   * export class MyObject {
   *  @func()
   *  public name: string
   * }
   * ```
   *
   * @example
   * ```ts
   * export type Example = {
   *   name: string
   * }
   * ```
   */
  public objects: DaggerObjectsBase = {}

  /**
   * An enum is typically a native TypeScript enum declared with the `enum` keyword.
   * Decorated classes using `@enumType()` are still supported for backward compatibility.
   *
   * @example
   * ```ts
   * export enum Example {
   *   A = "a",
   *   B = "b",
   * }
   * ```
   */
  public enums: DaggerEnumsBase = {}

  /**
   * An interface is declared using the `interface` keyword.
   *
   * @example
   * ```ts
   * export interface Example {
   *   foo: (): string
   *   asyncFoo: (): Promise<string>
   * }
   * ```
   */
  public interfaces: DaggerInterfaces = {}

  public description: string | undefined

  private references: References = {
    // Float is a special case, it's an alias of number but it serves to declare a float type
    // in the Dagger API.
    // So we auto register it because it will be detected as a referenced type by the introspector.
    float: { kind: TypeDefKind.FloatKind },
  }

  constructor(
    public name: string,
    private userModule: Module[],
    private ast: AST,
  ) {
    this.validateNoCoreTypeShadowing()

    const classObjects = this.findClasses()
    for (const classObject of classObjects) {
      // This only applies to cloud. If this is the true main object, it is correct
      // if this is a blueprint, the description will not matter in any situation
      const mainFileContent = classObject.file.getFullText()
      this.description = this.getDescription(mainFileContent)

      const daggerObject = new DaggerObject(classObject.node, this.ast)
      const objectName = classObject.node.name?.getText() || this.name

      this.objects[objectName] = daggerObject
      this.references[objectName] = {
        kind: TypeDefKind.ObjectKind,
        name: objectName,
      }

      this.resolveReferences(daggerObject.getReferences())
      this.propagateReferences()
    }
  }

  /**
   * Find the reference of the module and register it to the module references.
   *
   * To do so, we check the user module to find a corresponding symbol (name) for each of
   * typedef we support.
   * This only applies for:
   * - classes
   * - enums
   * - scalars
   * - interfaces
   *
   * If the reference is an object, a class or an interface, recursively find the references of the object.
   *
   * *Note*: If a class is referenced but not exported and not decorated with `@object()`, we throw an error
   * because we aim to be explicit. (TomChv: Should we change this behaviour?)
   */
  private resolveReferences(references: string[]) {
    if (references.length === 0) {
      return
    }

    for (const reference of references) {
      // If we already know that reference, we don't need to explore it again.
      if (this.references[reference]) {
        continue
      }

      const classRef = this.ast.findResolvedNodeByName(
        reference,
        ts.SyntaxKind.ClassDeclaration,
      )
      if (classRef) {
        if (this.ast.isGeneratedClientFile(classRef.file.fileName)) {
          this.references[reference] = {
            kind: TypeDefKind.ObjectKind,
            name: reference,
          }
          continue
        }

        if (
          this.ast.isNodeDecoratedWith(classRef.node, OBJECT_DECORATOR) ||
          this.ast.isNodeDecoratedWith(classRef.node, COLLECTION_DECORATOR)
        ) {
          const daggerObject = new DaggerObject(classRef.node, this.ast)
          this.objects[daggerObject.name] = daggerObject
          this.references[daggerObject.name] = {
            kind: TypeDefKind.ObjectKind,
            name: daggerObject.name,
          }

          this.resolveReferences(daggerObject.getReferences())

          continue
        }

        if (this.ast.isNodeDecoratedWith(classRef.node, ENUM_DECORATOR)) {
          const daggerEnum = new DaggerEnumClass(classRef.node, this.ast)
          this.enums[daggerEnum.name] = daggerEnum
          this.references[daggerEnum.name] = {
            kind: TypeDefKind.EnumKind,
            name: daggerEnum.name,
          }

          // There should be no references in enums.
          continue
        }

        throw new IntrospectionError(
          `class ${reference} in ${AST.getNodePosition(classRef.node)} is used by the module but not exposed with a dagger decorator.`,
        )
      }

      const enumRef = this.ast.findResolvedNodeByName(
        reference,
        ts.SyntaxKind.EnumDeclaration,
      )
      if (enumRef) {
        if (this.ast.isGeneratedClientFile(enumRef.file.fileName)) {
          this.references[reference] = {
            kind: TypeDefKind.EnumKind,
            name: reference,
          }
          continue
        }

        // Typescript enum declaration cannot be decorated, so we don't check it.
        const daggerEnum = new DaggerEnum(enumRef.node, this.ast)
        this.enums[daggerEnum.name] = daggerEnum
        this.references[daggerEnum.name] = {
          kind: TypeDefKind.EnumKind,
          name: daggerEnum.name,
        }

        // There should be no reference in enums.
        continue
      }

      const interfaceRef = this.ast.findResolvedNodeByName(
        reference,
        ts.SyntaxKind.InterfaceDeclaration,
      )
      if (interfaceRef) {
        const daggerInterface = new DaggerInterface(interfaceRef.node, this.ast)
        this.interfaces[daggerInterface.name] = daggerInterface
        this.references[daggerInterface.name] = {
          kind: TypeDefKind.InterfaceKind,
          name: daggerInterface.name,
        }
        this.resolveReferences(daggerInterface.getReferences())
        continue
      }

      const typeAliasRef = this.ast.findResolvedNodeByName(
        reference,
        ts.SyntaxKind.TypeAliasDeclaration,
      )
      if (typeAliasRef) {
        // The resolution is to big so we split it in a sub function.
        this.resolveTypeAlias(reference, typeAliasRef)

        continue
      }

      // Handle primitives here
      if (reference === "String") {
        throw new IntrospectionError(
          `Use of primitive 'String' type detected, please use 'string' instead.`,
        )
      }

      if (reference === "Boolean") {
        throw new IntrospectionError(
          `Use of primitive 'Boolean' type detected, please use 'boolean' instead.`,
        )
      }

      if (reference === "Number") {
        throw new IntrospectionError(
          `Use of primitive 'Number' type detected, please use 'number' instead.`,
        )
      }

      throw new IntrospectionError(
        `could not resolve type reference for ${reference}.`,
      )
    }
  }

  /**
   * Resolve type alias to the corresponding TypeDef.
   * A type might refer to anything typeable in TypeScript but right now we supports:
   * - `type Example = string`
   * - `type Example = { prop: string}`
   * - `type Example = number`
   * - `type Example = boolean`
   * - `type Example = void`
   *
   * If the reference is an object, we recursively resolve its references.
   * If the type cannot be resolved or is not supported, we throw an error.
   */
  private resolveTypeAlias(
    reference: string,
    typeAlias: ResolvedNodeWithSymbol<ts.SyntaxKind.TypeAliasDeclaration>,
  ) {
    const type = this.ast.getTypeFromTypeAlias(typeAlias.node)

    if (type.flags & ts.TypeFlags.String) {
      this.references[reference] = { kind: TypeDefKind.StringKind }

      return
    }

    if (type.flags & ts.TypeFlags.Number) {
      this.references[reference] = { kind: TypeDefKind.IntegerKind }

      return
    }

    if (type.flags & ts.TypeFlags.Boolean) {
      this.references[reference] = { kind: TypeDefKind.BooleanKind }

      return
    }

    if (type.flags & ts.TypeFlags.Void) {
      this.references[reference] = { kind: TypeDefKind.VoidKind }

      return
    }

    // Scalar are defined with string intersection such as `type MyScalar = string & { __MyScalar: never }`
    if (type.flags & ts.TypeFlags.Intersection) {
      this.references[reference] = {
        kind: TypeDefKind.ScalarKind,
        name: reference,
      }

      return
    }

    // A union used to land in the branch above and be registered as a scalar of
    // its own name, which nothing defines: the module then failed to load with
    // `find mod type for ... type: "<name>!"`, or — when the name happened to
    // match a core type — resolved to that type instead and failed at call time
    // with an error about a value the function never took.
    if (type.flags & ts.TypeFlags.Union) {
      throw new IntrospectionError(
        `Unsupported union type alias '${reference}' in ${typeAlias.file.fileName}. ` +
          `Declare an enum for a set of allowed values, or use a supported type.`,
      )
    }

    if (type.flags & ts.TypeFlags.Object) {
      if (this.ast.isGeneratedClientFile(typeAlias.file.fileName)) {
        this.references[reference] = {
          kind: TypeDefKind.ObjectKind,
          name: reference,
        }

        return
      }

      const daggerObject = new DaggerTypeObject(typeAlias.node, this.ast)
      this.objects[daggerObject.name] = daggerObject
      this.references[daggerObject.name] = {
        kind: TypeDefKind.ObjectKind,
        name: daggerObject.name,
      }

      this.resolveReferences(daggerObject.getReferences())

      return
    }

    throw new IntrospectionError(
      `could not resolve type reference for ${reference} at ${AST.getNodePosition(typeAlias.node)}`,
    )
  }

  /**
   * A type the module declares must not be named after a core type.
   *
   * The engine namespaces a module's types against other modules, not against
   * core, so a declaration sharing a core name does not get a namespace of its
   * own: the core type answers for it wherever the name is used. Nothing failed
   * — `type Volume = "loud" | "quiet"` generated a module taking a core Volume,
   * which `dagger call ... --volume loud` then rejected as a volume address,
   * and `@object class Volume` generated an object whose own fields could not be
   * queried. Go and Python modules collide the same way, so this is refused
   * here rather than worked around.
   *
   * Checked before anything else: it has to report the declaration the user
   * wrote, and by the time a reference to it is resolved the core type has
   * already answered for the name.
   *
   * Every declaration counts, decorated or not and used by the module's API or
   * not. An undecorated `class Volume` is the same silent failure one `@func`
   * argument later, and a rule that holds for a declaration rather than for a
   * use does not depend on which of the two the resolver happens to find first.
   */
  private validateNoCoreTypeShadowing(): void {
    const coreTypes = this.ast.coreTypeNames()
    if (coreTypes.size === 0) {
      return
    }

    const declarations = [
      ...this.ast.findAllDeclarations(ts.SyntaxKind.ClassDeclaration),
      ...this.ast.findAllDeclarations(ts.SyntaxKind.EnumDeclaration),
      ...this.ast.findAllDeclarations(ts.SyntaxKind.InterfaceDeclaration),
      ...this.ast.findAllDeclarations(ts.SyntaxKind.TypeAliasDeclaration),
    ]

    for (const { node, file } of declarations) {
      const name = node.name?.getText()
      if (
        !name ||
        !coreTypes.has(name) ||
        this.ast.isGeneratedClientFile(file.fileName)
      ) {
        continue
      }

      throw new IntrospectionError(
        `${declarationKind(node)} ${name} at ${AST.getNodePosition(node)} shadows the core Dagger type ${name}. ` +
          `A module's types are namespaced against other modules but not against core, so the core type answers for that name wherever it is used. ` +
          `Rename the declaration.`,
      )
    }
  }

  /**
   * A decorated class name must be unique across the module's files.
   *
   * Every declaration is keyed by its name from here to the engine, so a second
   * class with the same name does not collide: it replaces the first. Nothing
   * fails, the module generates, and the functions of the class that lost are
   * simply missing at call time — reported as an unknown command, pointing
   * nowhere near the two declarations.
   *
   * Checked before the main class lookup below, which returns the first match
   * and would hide the second declaration from this loop entirely.
   */
  private validateUniqueClassNames(
    declarations: ResolvedNodeWithSymbol<ts.SyntaxKind.ClassDeclaration>[],
  ): void {
    const seen = new Map<string, ts.ClassDeclaration>()

    for (const { node } of declarations) {
      if (
        !this.ast.isNodeDecoratedWith(node, OBJECT_DECORATOR) &&
        !this.ast.isNodeDecoratedWith(node, COLLECTION_DECORATOR) &&
        !this.ast.isNodeDecoratedWith(node, ENUM_DECORATOR)
      ) {
        continue
      }

      const name = node.name?.getText()
      if (!name) {
        continue
      }

      const previous = seen.get(name)
      if (previous) {
        throw new IntrospectionError(
          `class ${name} is declared twice, at ${AST.getNodePosition(previous)} and at ${AST.getNodePosition(node)}: dagger declarations are keyed by name, so one would silently replace the other. Rename one of them.`,
        )
      }

      seen.set(name, node)
    }
  }

  /**
   * Find the classes in the AST. Returns only our main class if it exists
   */
  private findClasses(): ResolvedNodeWithSymbol<ts.SyntaxKind.ClassDeclaration>[] {
    const allClassDeclarations = this.ast.findAllDeclarations(
      ts.SyntaxKind.ClassDeclaration,
    )

    this.validateUniqueClassNames(allClassDeclarations)

    const allClasses: ResolvedNodeWithSymbol<ts.SyntaxKind.ClassDeclaration>[] =
      []
    for (const classDecl of allClassDeclarations) {
      const convertedDecl =
        classDecl as ResolvedNodeWithSymbol<ts.SyntaxKind.ClassDeclaration>

      // Check if the class matches this.name and return only that if so
      if (
        convertedDecl.node.name &&
        convertedDecl.node.name.getText() === this.name
      ) {
        return [convertedDecl]
      }

      // or we return all classes decorated with @object
      if (
        this.ast.isNodeDecoratedWith(classDecl.node, OBJECT_DECORATOR) ||
        this.ast.isNodeDecoratedWith(classDecl.node, COLLECTION_DECORATOR)
      ) {
        allClasses.push(convertedDecl)
      }
    }
    return allClasses
  }

  /**
   * Recursively propagate references to all objects properties and functions.
   */
  private propagateReferences() {
    for (const object of Object.values(this.objects)) {
      object.propagateReferences(this.references)
    }

    for (const interface_ of Object.values(this.interfaces)) {
      interface_.propagateReferences(this.references)
    }
  }

  /**
   * Get the top level comment of the file that contains the module entrypoint.
   */
  private getDescription(sourceFileContent: string): string | undefined {
    const regex = /^(?!.*import)[\s]*\/\*\*([\s\S]*?)\*\//
    const match = sourceFileContent.match(regex)

    if (!match) {
      return undefined
    }

    const comment = match[1]
      .split("\n")
      .map((line) => line.replace(/^\s*\*\s?/, ""))
      .join("\n")

    return comment.trim()
  }

  toJSON() {
    return {
      name: this.name,
      description: this.description,
      objects: this.objects,
      enums: this.enums,
      interfaces: this.interfaces,
    }
  }
}
