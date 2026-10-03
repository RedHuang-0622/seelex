import FreeCAD as App
doc = App.getDocument('FlangeCoupling')
obj = doc.getObject('FlangeRibbed')
shape = obj.Shape
edges = shape.Edges

# Classify edges
for i, e in enumerate(edges):
    try:
        cog = e.CenterOfGravity
        r = (cog.x**2 + cog.y**2)**0.5
        length = e.Length
        etype = type(e.Curve).__name__
        # Only show interesting edges
        if r > 50 or (r < 30 and r > 20) or length > 10:
            print(f'Edge {i}: r={r:.1f}, len={length:.1f}, type={etype}')
    except:
        pass
