import FreeCAD as App
doc = App.getDocument('FlangeCoupling')
obj = doc.getObject('FlangeRibbed')
edges = obj.Shape.Edges

# Try to fillet ALL edges with R2
all_edges = []
for i in range(len(edges)):
    all_edges.append((i+1, 2.0, 2.0))

print(f'Trying to fillet {len(all_edges)} edges...')
fillet = doc.addObject('Part::Fillet', 'Fillets')
fillet.Base = obj
fillet.Edges = all_edges
doc.recompute()
print('Fillet applied to all edges!')
