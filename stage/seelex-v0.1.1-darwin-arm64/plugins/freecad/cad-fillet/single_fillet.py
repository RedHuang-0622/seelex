import FreeCAD as App
doc = App.getDocument('FlangeCoupling')
obj = doc.getObject('FlangeRibbed')
edges = obj.Shape.Edges
ext_edges = []
int_edges = []
for i, e in enumerate(edges):
    try:
        cog = e.CenterOfGravity
        r = (cog.x**2 + cog.y**2)**0.5
        if r > 55:
            ext_edges.append((i+1, 3.0, 3.0))
        elif r > 1:
            int_edges.append((i+1, 2.0, 2.0))
    except:
        pass

print(f'External (R3): {len(ext_edges)} edges')
print(f'Internal (R2): {len(int_edges)} edges')

# Create fillet with external edges first (R3)
all_edges = ext_edges + int_edges
fillet = doc.addObject('Part::Fillet', 'Fillets')
fillet.Base = obj
fillet.Edges = all_edges[:50]  # Limit to avoid issues
doc.recompute()
print('Fillet applied')
