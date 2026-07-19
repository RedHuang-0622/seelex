import FreeCAD as App
doc = App.getDocument('FlangeCoupling')
try:
    doc.removeObject('Fillets')
    doc.recompute()
    print('Fillet removed')
except:
    print('No fillet to remove')

obj = doc.getObject('FlangeRibbed')
edges = obj.Shape.Edges
print(f'Total edges: {len(edges)}')
for i, e in enumerate(edges):
    try:
        c = e.CenterOfGravity
        r = (c.x**2 + c.y**2)**0.5
        t = type(e.Curve).__name__
        if e.Length > 20:
            print(f'Edge {i}: r={r:.1f} len={e.Length:.1f} {t}')
    except:
        pass
