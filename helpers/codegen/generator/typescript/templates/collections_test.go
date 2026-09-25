package templates

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCollectionDangMetadata(t *testing.T) {
	obj := &TypedefObject{Name: "Items", IsCollection: true,
		Properties: map[string]*TypedefProperty{
			"names":     {Name: "names", Alias: "paths", IsExposed: true, IsCollectionKeys: true, Type: &TypedefType{Kind: KindList, TypeDef: &TypedefType{Kind: KindString}}},
			"selection": {Name: "selection", IsExposed: true, IsCollectionDelta: true, Type: &TypedefType{Kind: KindObject, Name: "CollectionDelta"}},
		},
		Methods: map[string]*TypedefFunction{
			"item": {Name: "item", Alias: "lookup", IsCollectionGet: true, ReturnType: &TypedefType{Kind: KindObject, Name: "Item"}},
		},
	}
	c := &dangFuncCtx{module: &TypedefModule{Name: "Main"}}
	got := c.dangObjectEntry(obj)
	require.Contains(t, got, ".withCollection")
	require.Contains(t, got, `.withCollectionKeys("paths")`)
	require.Contains(t, got, `.withCollectionDelta("selection")`)
	require.Contains(t, got, `.withCollectionGet("lookup")`)
}
