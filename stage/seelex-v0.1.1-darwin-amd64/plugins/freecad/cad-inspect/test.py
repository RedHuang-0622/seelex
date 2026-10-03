import FreeCAD as App
doc = App.getDocument('FlangeCoupling')
obj = doc.getObject('FlangeRibbed')

# Try filleting just edge 78 (r=45, Circle, len=62.8 - likely a bolt hole edge)
fillet = doc.addObject('Part::Fillet', 'TestFillet')
fillet.Base = obj
fillet.Edges = [(78, 2.0, 2.0)]
doc.recompute()
try:
    print('Valid:', fillet.Shape.isValid())
    print('Volume:', fillet.Shape.Volume)
except Exception as e:
    print('Error:', e)
