import FreeCAD as App
doc = App.getDocument('FlangeCoupling')
obj = doc.getObject('FlangeRibbed')
edges = obj.Shape.Edges

# Select edges: length > 3mm, radius from Z-axis between 20-58mm
sel = []
for i, e in enumerate(edges):
    try:
        cog = e.CenterOfGravity
        r = (cog.x**2 + cog.y**2)**0.5
        if e.Length > 3 and 20 < r < 58:
            sel.append((i+1, 2.0, 2.0))
    except:
        pass

print(f'Selected {len(sel)} edges')
fillet = doc.addObject('Part::Fillet', 'Fillets')
fillet.Base = obj
fillet.Edges = sel
doc.recompute()
print('Fillet applied')
try:
    print(f'Valid: {fillet.Shape.isValid()}')
except Exception as e:
    print(f'Shape error: {e}')
